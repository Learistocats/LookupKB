package server

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/Learistocats/LookupKB/api/internal/db/dbgen"
	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

const (
	testIssuer   = "https://authentik.example.com/application/o/lookupkb/"
	testAudience = "lookupkb-client-id"
	testSub      = "ak-sub-123"
)

var testKey = mustRSAKey()

func mustRSAKey() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return k
}

// fakeDocStore records the params it is called with and returns canned rows, so
// handler behaviour (owner scoping, status codes) is tested without a database.
type fakeDocStore struct {
	createArg dbgen.CreateDraftParams
	saveArg   dbgen.SaveDraftParams
	getArg    dbgen.GetDraftParams

	createDoc dbgen.Document
	saveDoc   dbgen.Document
	getDoc    dbgen.Document

	createErr error
	saveErr   error
	getErr    error
}

func (f *fakeDocStore) CreateDraft(_ context.Context, arg dbgen.CreateDraftParams) (dbgen.Document, error) {
	f.createArg = arg
	return f.createDoc, f.createErr
}

func (f *fakeDocStore) SaveDraft(_ context.Context, arg dbgen.SaveDraftParams) (dbgen.Document, error) {
	f.saveArg = arg
	return f.saveDoc, f.saveErr
}

func (f *fakeDocStore) GetDraft(_ context.Context, arg dbgen.GetDraftParams) (dbgen.Document, error) {
	f.getArg = arg
	return f.getDoc, f.getErr
}

// callerUUID is the internal user id the fake upserter returns for the verified
// caller; handlers must use it (not the email/sub) as author_id.
var callerUUID = uuid.New()

func pgUUID(u uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: u, Valid: true} }

// draftDoc builds a canned draft row owned by callerUUID.
func draftDoc(id uuid.UUID, source string) dbgen.Document {
	src := source
	return dbgen.Document{
		ID:        pgUUID(id),
		Slug:      "draft-deadbeef",
		AuthorID:  pgUUID(callerUUID),
		Status:    dbgen.DocumentStatusDraft,
		SourceMd:  &src,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
}

// newDraftServer wires a Server with the real auth middleware (static-key
// verifier) and the given documents store, so requests exercise the full
// authenticated route path.
func newDraftServer(t *testing.T, store DocumentStore) *Server {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)

	ks := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{testKey.Public()}}
	verifier := auth.VerifierFrom(oidc.NewVerifier(testIssuer, ks, &oidc.Config{ClientID: testAudience}))
	up := &fakeUpserter{userID: pgUUID(callerUUID)}
	mw := auth.NewMiddleware(verifier, up, slogDiscard())

	opts := []Option{WithAuth(mw)}
	if store != nil {
		opts = append(opts, WithDocuments(store))
	}
	return New(cfg, slogDiscard(), "test", opts...)
}

// fakeUpserter returns a fixed internal user id for any verified caller.
type fakeUpserter struct{ userID pgtype.UUID }

func (f *fakeUpserter) UpsertUser(_ context.Context, _ dbgen.UpsertUserParams) (dbgen.User, error) {
	return dbgen.User{ID: f.userID}, nil
}

func bearer(t *testing.T) string {
	t.Helper()
	claims := map[string]any{
		"iss": testIssuer,
		"aud": testAudience,
		"sub": testSub,
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Add(-time.Minute).Unix(),
	}
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: testKey},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	require.NoError(t, err)
	jws, err := signer.Sign(payload)
	require.NoError(t, err)
	raw, err := jws.CompactSerialize()
	require.NoError(t, err)
	return "Bearer " + raw
}

func doReq(t *testing.T, s *Server, method, target, body string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, target, r)
	req.Header.Set("Authorization", bearer(t))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.App().Test(req)
	require.NoError(t, err)
	return resp
}

func TestCreateDraft_Returns201OwnedDraft(t *testing.T) {
	id := uuid.New()
	store := &fakeDocStore{createDoc: draftDoc(id, "")}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodPost, "/api/v1/documents", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusCreated, resp.StatusCode)
	// author_id must be the caller's internal id, and a placeholder slug is set.
	require.Equal(t, pgUUID(callerUUID), store.createArg.AuthorID)
	require.True(t, strings.HasPrefix(store.createArg.Slug, "draft-"))

	var got draftResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, "draft", got.Status)
	require.Equal(t, id.String(), got.ID)
}

func TestSaveDraft_PersistsBodyScopedToCaller(t *testing.T) {
	id := uuid.New()
	store := &fakeDocStore{saveDoc: draftDoc(id, "# hello")}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodPut, "/api/v1/drafts/"+id.String(), `{"source_md":"# hello"}`)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, pgUUID(id), store.saveArg.ID)
	require.Equal(t, pgUUID(callerUUID), store.saveArg.AuthorID)
	require.NotNil(t, store.saveArg.SourceMd)
	require.Equal(t, "# hello", *store.saveArg.SourceMd)
}

// AC-2.2: an empty/partial metadata save must succeed with no validation error.
func TestSaveDraft_EmptyBodySucceeds(t *testing.T) {
	id := uuid.New()
	store := &fakeDocStore{saveDoc: draftDoc(id, "")}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodPut, "/api/v1/drafts/"+id.String(), `{}`)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Nil(t, store.saveArg.Title)
	require.Nil(t, store.saveArg.SourceMd)
}

func TestGetDraft_ReturnsOwnDraft(t *testing.T) {
	id := uuid.New()
	store := &fakeDocStore{getDoc: draftDoc(id, "# persisted")}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodGet, "/api/v1/drafts/"+id.String(), "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, pgUUID(callerUUID), store.getArg.AuthorID)

	var got draftResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.NotNil(t, got.SourceMd)
	require.Equal(t, "# persisted", *got.SourceMd)
}

// A draft not owned by the caller (or missing) yields no row → 404, identical
// for "not yours" and "does not exist" so ownership is never leaked.
func TestGetDraft_OtherUsersDraftIs404(t *testing.T) {
	store := &fakeDocStore{getErr: pgx.ErrNoRows}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodGet, "/api/v1/drafts/"+uuid.NewString(), "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestSaveDraft_OtherUsersDraftIs404(t *testing.T) {
	store := &fakeDocStore{saveErr: pgx.ErrNoRows}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodPut, "/api/v1/drafts/"+uuid.NewString(), `{"source_md":"x"}`)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGetDraft_MalformedIDIs400(t *testing.T) {
	store := &fakeDocStore{}
	s := newDraftServer(t, store)

	resp := doReq(t, s, http.MethodGet, "/api/v1/drafts/not-a-uuid", "")
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// Without a documents store the draft routes are not registered at all (404),
// while /me on the same authed group still works.
func TestDraftRoutesUnregisteredWithoutStore(t *testing.T) {
	s := newDraftServer(t, nil)

	resp := doReq(t, s, http.MethodPost, "/api/v1/documents", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// Unauthenticated requests never reach the handler/store (middleware 401).
func TestDraftRoutesRequireAuth(t *testing.T) {
	store := &fakeDocStore{}
	s := newDraftServer(t, store)

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/documents", nil)
	resp, err := s.App().Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

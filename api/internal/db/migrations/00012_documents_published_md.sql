-- +goose Up
-- LOO-38: the published Markdown snapshot.
--
-- documents.source_md is the WORKING COPY: a draft save overwrites it in place,
-- even after publish (re-publish is what makes an edit public). Serving it as the
-- "published Markdown" (public API ?format=markdown, MCP get_document) would leak
-- unpublished edits. published_md is the exact source that produced the cached
-- rendered_html: written by PublishDocument inside the publish transaction and
-- cleared by UnpublishDocument. Reads serve it as a straight column fetch, like
-- rendered_html. No version history: a re-publish overwrites it in place.
ALTER TABLE documents ADD COLUMN published_md text;

-- Backfill, BEST-EFFORT: nothing recorded which source an already-published entry
-- was rendered from, so its current working copy is taken. That equals the
-- published source unless the author edited the entry after its last publish
-- without re-publishing; such an edit is then served by ?format=markdown until the
-- next (re-)publish refreshes published_md. rendered_html (the reader view, the
-- HTML/text formats and the search index) is unaffected.
UPDATE documents
SET published_md = COALESCE(source_md, '')
WHERE status = 'published'
  AND deleted_at IS NULL;

-- +goose Down
ALTER TABLE documents DROP COLUMN published_md;

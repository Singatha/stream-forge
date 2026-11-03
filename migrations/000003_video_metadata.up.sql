CREATE TABLE videos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(200) NOT NULL,
    description VARCHAR(5000) NOT NULL DEFAULT '',
    visibility TEXT NOT NULL DEFAULT 'PRIVATE',
    processing_status TEXT NOT NULL DEFAULT 'CREATED',
    view_count BIGINT NOT NULL DEFAULT 0,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT videos_title_length CHECK (char_length(title) BETWEEN 1 AND 200),
    CONSTRAINT videos_description_length CHECK (char_length(description) <= 5000),
    CONSTRAINT videos_visibility_check CHECK (visibility IN ('PUBLIC', 'PRIVATE', 'UNLISTED')),
    CONSTRAINT videos_processing_status_check CHECK (
        processing_status IN ('CREATED', 'UPLOADING', 'UPLOADED', 'PROCESSING', 'READY', 'FAILED')
    ),
    CONSTRAINT videos_view_count_check CHECK (view_count >= 0)
);

CREATE INDEX videos_owner_created_idx
    ON videos (owner_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX videos_public_created_idx
    ON videos (created_at DESC, id DESC)
    WHERE visibility = 'PUBLIC' AND deleted_at IS NULL;

CREATE INDEX videos_published_at_idx
    ON videos (published_at DESC, id DESC)
    WHERE visibility = 'PUBLIC' AND published_at IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX videos_processing_status_idx
    ON videos (processing_status, created_at)
    WHERE deleted_at IS NULL;


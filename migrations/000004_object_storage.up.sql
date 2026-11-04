CREATE TABLE video_assets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    asset_type TEXT NOT NULL,
    object_key TEXT NOT NULL UNIQUE,
    expected_content_type TEXT NOT NULL,
    expected_size_bytes BIGINT NOT NULL,
    content_type TEXT,
    size_bytes BIGINT,
    etag TEXT,
    storage_status TEXT NOT NULL DEFAULT 'PENDING',
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT video_assets_type_check CHECK (
        asset_type IN ('SOURCE', 'HLS_MASTER', 'HLS_VARIANT', 'THUMBNAIL')
    ),
    CONSTRAINT video_assets_storage_status_check CHECK (storage_status IN ('PENDING', 'READY')),
    CONSTRAINT video_assets_expected_size_check CHECK (expected_size_bytes > 0),
    CONSTRAINT video_assets_size_check CHECK (size_bytes IS NULL OR size_bytes > 0),
    CONSTRAINT video_assets_object_key_check CHECK (
        object_key ~ '^videos/[0-9a-f-]{36}/[a-z0-9._/-]+$'
        AND object_key !~ '(^|/)\.\.(/|$)'
    ),
    CONSTRAINT video_assets_ready_metadata_check CHECK (
        (storage_status = 'PENDING' AND completed_at IS NULL)
        OR
        (storage_status = 'READY' AND completed_at IS NOT NULL AND content_type IS NOT NULL
            AND size_bytes IS NOT NULL AND etag IS NOT NULL)
    )
);

CREATE UNIQUE INDEX video_assets_source_video_idx
    ON video_assets (video_id)
    WHERE asset_type = 'SOURCE';

CREATE INDEX video_assets_video_idx ON video_assets (video_id, asset_type);


CREATE TABLE heatmap_cells (
    window_start  TIMESTAMPTZ      NOT NULL,
    resolution    SMALLINT         NOT NULL CHECK (resolution BETWEEN 0 AND 15),
    category_slug TEXT             NOT NULL,
    h3            BIGINT           NOT NULL,
    count         INTEGER          NOT NULL CHECK (count >= 0),
    value         DOUBLE PRECISION NOT NULL CHECK (value >= 0),
    updated_at    TIMESTAMPTZ      NOT NULL,
    CONSTRAINT heatmap_cells_key UNIQUE (window_start, resolution, category_slug, h3)
);

CREATE INDEX heatmap_cells_lookup_idx ON heatmap_cells (resolution, window_start, h3);

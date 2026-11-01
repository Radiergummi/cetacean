CREATE TABLE annotations (
    id               INTEGER PRIMARY KEY,
    version          INTEGER NOT NULL,
    at               INTEGER NOT NULL,
    until            INTEGER,
    text             TEXT    NOT NULL,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER,
    actor_subject    TEXT,
    actor_name       TEXT,
    actor_provider   TEXT,
    actor_client     TEXT,
    actor_transport  TEXT,
    editor_subject   TEXT,
    editor_name      TEXT,
    editor_provider  TEXT,
    editor_client    TEXT,
    editor_transport TEXT
);

CREATE TABLE annotation_tags (
    annotation_id INTEGER NOT NULL,
    tag           TEXT    NOT NULL,
    PRIMARY KEY (annotation_id, tag)
);

CREATE TABLE annotation_resources (
    annotation_id INTEGER NOT NULL,
    type          TEXT    NOT NULL,
    resource_id   TEXT    NOT NULL,
    name          TEXT,
    service       TEXT,
    stack         TEXT,
    PRIMARY KEY (annotation_id, type, resource_id)
);

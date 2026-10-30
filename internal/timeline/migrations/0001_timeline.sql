CREATE TABLE timeline (
    id               INTEGER PRIMARY KEY,
    kind             TEXT    NOT NULL,
    at               INTEGER NOT NULL,
    until            INTEGER,
    type             TEXT,
    action           TEXT,
    resource_id      TEXT,
    name             TEXT,
    summary          TEXT,
    service          TEXT,
    stack            TEXT,
    service_id       TEXT,
    actor_subject    TEXT,
    actor_name       TEXT,
    actor_provider   TEXT,
    actor_client     TEXT,
    actor_transport  TEXT
);

CREATE INDEX timeline_at ON timeline (at);

CREATE TABLE coverage (
    id          INTEGER PRIMARY KEY,
    started_at  INTEGER NOT NULL,
    seen_at     INTEGER NOT NULL
);

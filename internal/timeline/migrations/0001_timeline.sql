CREATE TABLE timeline (
    id               BIGINT PRIMARY KEY,
    kind             TEXT    NOT NULL,
    at               BIGINT  NOT NULL,
    until            BIGINT,
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

CREATE INDEX timeline_resource ON timeline (resource_id, id);
CREATE INDEX timeline_service ON timeline (service_id, id);
CREATE INDEX timeline_at ON timeline (at);

CREATE TABLE coverage (
    id          BIGINT PRIMARY KEY,
    started_at  BIGINT NOT NULL,
    seen_at     BIGINT NOT NULL
);

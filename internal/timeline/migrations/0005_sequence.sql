CREATE TABLE sequence (
    id        INTEGER PRIMARY KEY CHECK (id = 1),
    last      INTEGER NOT NULL,
    instance  TEXT    NOT NULL
);

INSERT INTO sequence (id, last, instance)
SELECT 1,
       max((SELECT coalesce(max(id), 0) FROM timeline),
           (SELECT coalesce(max(version), 0) FROM annotations)),
       lower(hex(randomblob(16)));

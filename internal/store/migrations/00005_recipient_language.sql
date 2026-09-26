-- +goose Up

-- A person's preferences, as opposed to their places. The language is kept
-- per person because every alert to them should be in the language they read.
CREATE TABLE recipients (
    channel    text        NOT NULL CHECK (channel IN ('telegram')),
    recipient  text        NOT NULL CHECK (length(btrim(recipient)) > 0),
    language   text        NOT NULL CHECK (language IN ('en', 'th')),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (channel, recipient)
);

-- +goose Down

DROP TABLE recipients;

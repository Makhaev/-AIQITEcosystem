CREATE TABLE module_files (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    module_id bigint NOT NULL REFERENCES program_modules(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    content bytea NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 10485760),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX module_files_module_id_idx ON module_files(module_id, id);

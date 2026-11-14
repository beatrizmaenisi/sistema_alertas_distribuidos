CREATE TABLE IF NOT EXISTS leituras_sensor (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    dispositivo_hash CHAR(64) NOT NULL,
    medido_em        TIMESTAMPTZ NOT NULL,
    temperatura_c    DECIMAL(5,2),
    umidade_pct      DECIMAL(5,2) CHECK (umidade_pct BETWEEN 0 AND 100),
    chuva_mm_h       DECIMAL(6,2) CHECK (chuva_mm_h >= 0),
    latitude         DECIMAL(10,8),
    longitude        DECIMAL(11,8),
    modelo           VARCHAR(32),
    recebido_em      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (dispositivo_hash, medido_em)
);
CREATE INDEX IF NOT EXISTS idx_leituras_sensor_medido_em ON leituras_sensor (medido_em);

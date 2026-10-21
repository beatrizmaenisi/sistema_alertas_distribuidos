CREATE TYPE tipo_sensor_enum AS ENUM ('CHUVA', 'TEMPERATURA', 'HUMIDADE', 'BARULHO', 'VIBRACAO');
CREATE TYPE sensor_status_enum AS ENUM ('ATIVO', 'INATIVO', 'MANUTENCAO', 'DESATIVADO');
CREATE TYPE alerta_nivel_enum AS ENUM ('INFO', 'ATENCAO', 'EMERGENCIA');
CREATE TYPE alerta_status_enum AS ENUM ('ATIVO', 'RECONHECIDO', 'RESOLVIDO', 'EXPIRADO');
CREATE TYPE tipo_autor_enum AS ENUM ('USUARIO', 'SENSOR');

CREATE TABLE servidor (
    id_no           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    nome            VARCHAR(50) NOT NULL UNIQUE,
    ultimo_recebido TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE credibilidade (
    id_credibilidade UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    score            DECIMAL(4,3) NOT NULL CHECK (score BETWEEN 0 AND 1),
    versao_modelo    VARCHAR(50),
    avaliado_em      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sensor (
    id_sensor      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cod_estacao    VARCHAR(20) NOT NULL UNIQUE,
    nome           VARCHAR(100) NOT NULL,
    municipio      VARCHAR(60) NOT NULL,
    uf             CHAR(2) NOT NULL,
    tipo           tipo_sensor_enum NOT NULL DEFAULT 'CHUVA',
    status         sensor_status_enum NOT NULL DEFAULT 'ATIVO',
    lat            DECIMAL(10,8) NOT NULL,
    lng            DECIMAL(11,8) NOT NULL,
    ultimo_enviado TIMESTAMPTZ
);

CREATE TABLE leitura (
    id_sensor UUID NOT NULL REFERENCES sensor(id_sensor),
    datahora  TIMESTAMPTZ NOT NULL,
    valor_mm  DECIMAL(6,2) NOT NULL CHECK (valor_mm >= 0),
    PRIMARY KEY (id_sensor, datahora)
);
CREATE INDEX idx_leitura_datahora ON leitura (datahora);

CREATE TABLE alerta (
    id_alerta           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    id_no_origem        UUID REFERENCES servidor(id_no),
    autor               VARCHAR(64) NOT NULL,
    tipo_autor          tipo_autor_enum NOT NULL,
    nivel               alerta_nivel_enum NOT NULL,
    status              alerta_status_enum NOT NULL DEFAULT 'ATIVO',
    lat                 DECIMAL(10,8) NOT NULL,
    lng                 DECIMAL(11,8) NOT NULL,
    raio                DECIMAL(10,2) NOT NULL CHECK (raio > 0),
    comentario          TEXT,
    chave_dedup         VARCHAR(64) NOT NULL UNIQUE,
    n_relatos           INTEGER NOT NULL DEFAULT 1 CHECK (n_relatos >= 1),
    sensor_corroboracao BOOLEAN NOT NULL DEFAULT FALSE,
    dist_sensor_m       DECIMAL(10,1),
    credibilidade_id    UUID UNIQUE REFERENCES credibilidade(id_credibilidade),
    criado              TIMESTAMPTZ NOT NULL DEFAULT now(),
    expira              TIMESTAMPTZ NOT NULL,
    atualizado_em       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (expira > criado)
);
CREATE INDEX idx_alerta_atualizado ON alerta (atualizado_em);
CREATE INDEX idx_alerta_status ON alerta (status);

CREATE TABLE usuario (
    id_usuario       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hash_dispositivo CHAR(64) NOT NULL UNIQUE,
    token            VARCHAR(512)
);
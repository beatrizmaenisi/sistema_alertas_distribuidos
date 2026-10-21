import os

from dotenv import load_dotenv
from sqlalchemy import create_engine, text
from sqlalchemy.engine import URL

import config

# lê o .env da raiz do projeto, onde está a PG_SENHA
load_dotenv(config.raiz / ".env")

# guarda uma conexão (engine) por banco, para não recriar a cada chamada
_engines = {}


def conectar(nome_banco):
    if nome_banco not in config.bancos:
        raise ValueError(f"Banco desconhecido: {nome_banco}. Use um de {config.bancos}")

    senha = os.getenv("PG_SENHA")
    if not senha:
        raise RuntimeError("PG_SENHA não encontrada. Confira o arquivo .env na raiz do projeto.")

    if nome_banco not in _engines:
        url = URL.create(
            "postgresql+psycopg",
            username=config.pg_usuario,
            password=senha,
            host=config.pg_host,
            port=config.pg_porta,
            database=nome_banco,
        )
        _engines[nome_banco] = create_engine(url)
    return _engines[nome_banco]


def testar(nome_banco):
    with conectar(nome_banco).connect() as conexao:
        return conexao.execute(text("SELECT current_database(), version()")).one()
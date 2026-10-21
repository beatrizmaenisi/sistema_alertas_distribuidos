import pandas as pd

import config

colunas_brutas = [
    "municipio", "codEstacao", "uf", "nomeEstacao",
    "latitude", "longitude", "datahora", "valorMedida",
]
colunas_derivadas = ["datahora_local"]


def _checar_colunas(df, colunas, nome):
    faltando = [c for c in colunas if c not in df.columns]
    if faltando:
        raise ValueError(f"{nome}: colunas ausentes: {faltando}")


def carregar_cenario(caminho):
    df = pd.read_csv(caminho, sep=";", decimal=",", encoding="utf-8")
    _checar_colunas(df, colunas_brutas, str(caminho))

    # datahora fica em UTC, sem fuso anexado, igual ao que usamos no notebook
    df["datahora"] = pd.to_datetime(df["datahora"])

    # horário local derivado (UTC-3), também sem fuso anexado
    df["datahora_local"] = (
        df["datahora"]
        .dt.tz_localize(config.fuso_arquivo)
        .dt.tz_convert(config.fuso_local)
        .dt.tz_localize(None)
    )
    return df


def validar_schema(df, nome="tabela"):
    _checar_colunas(df, colunas_brutas + colunas_derivadas, nome)

    if df.empty:
        raise ValueError(f"{nome}: tabela vazia")

    for col in ["latitude", "longitude", "valorMedida"]:
        if not pd.api.types.is_numeric_dtype(df[col]):
            raise ValueError(f"{nome}: coluna {col} não é numérica ({df[col].dtype})")

    for col in ["datahora", "datahora_local"]:
        if not pd.api.types.is_datetime64_any_dtype(df[col]):
            raise ValueError(f"{nome}: coluna {col} não é data/hora ({df[col].dtype})")

    return df


def carregar_todos():
    tabelas = {}
    for c in config.cenarios:
        caminho = config.dados_raw / c["arquivo"]
        if not caminho.exists():
            raise FileNotFoundError(f"Cenário {c['id']}: arquivo não encontrado em {caminho}")
        df = carregar_cenario(caminho)
        validar_schema(df, nome=c["id"])
        tabelas[c["id"]] = df
    return tabelas
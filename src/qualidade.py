import pandas as pd

import config


def resumo_nulos(df):
    return pd.DataFrame({
        "nulos": df.isna().sum(),
        "pct": (df.isna().mean() * 100).round(2),
    })


def contar_duplicatas(df):
    return {
        "exatas": int(df.duplicated().sum()),
        "estacao_datahora": int(df.duplicated(subset=["codEstacao", "datahora"]).sum()),
    }


def valores_invalidos(df):
    # chuva negativa é fisicamente impossível
    return {"chuva_negativa": int((df["valorMedida"] < 0).sum())}


def cobertura_por_estacao(df):
    leituras = df.groupby("codEstacao").size()
    nomes = df.groupby("codEstacao")["nomeEstacao"].first()
    mediana = leituras.median()

    tab = pd.DataFrame({
        "nomeEstacao": nomes,
        "leituras": leituras,
        "pct_da_mediana": leituras / mediana * 100,
    })
    return tab.sort_values("pct_da_mediana")


def estacoes_baixa_cobertura(df, limiar_pct=None):
    if limiar_pct is None:
        limiar_pct = config.limiar_cobertura
    tab = cobertura_por_estacao(df)
    return tab[tab["pct_da_mediana"] < limiar_pct]


def filtrar_estacoes(df, cods_excluir):
    return df[~df["codEstacao"].isin(cods_excluir)].copy()


def aplicar_filtro_cobertura(df, limiar_pct=None):
    excluidas = estacoes_baixa_cobertura(df, limiar_pct)
    return filtrar_estacoes(df, excluidas.index), excluidas
import pandas as pd

import config


def calcular_gaps(df):
    df = df.sort_values(["codEstacao", "datahora"]).copy()
    df["gap"] = df.groupby("codEstacao")["datahora"].diff()
    intervalo = df.groupby("codEstacao")["gap"].median()
    df["intervalo_esperado"] = df["codEstacao"].map(intervalo)
    df["inicio_gap"] = df["datahora"] - df["gap"]
    return df


def _filtrar_buracos(gaps, multiplicador):
    sel = gaps["gap"] > gaps["intervalo_esperado"] * multiplicador
    cols = ["nomeEstacao", "codEstacao", "inicio_gap", "datahora", "gap", "intervalo_esperado"]
    return gaps.loc[sel, cols]


def intervalo_tipico(df):
    gaps = calcular_gaps(df)
    return gaps.groupby("codEstacao")["intervalo_esperado"].first()


def detectar_buracos(df, multiplicador=None):
    if multiplicador is None:
        multiplicador = config.mult_buraco
    return _filtrar_buracos(calcular_gaps(df), multiplicador)


def taxa_buracos(df, buracos):
    return len(buracos) / len(df) * 100


def taxa_por_multiplicador(df, multiplicadores=None):
    if multiplicadores is None:
        multiplicadores = config.mult_buraco_teste
    gaps = calcular_gaps(df)
    return {m: len(_filtrar_buracos(gaps, m)) / len(df) * 100 for m in multiplicadores}


def buracos_na_janela(buracos, inicio, fim):
    b = buracos.copy()
    b["fim_gap"] = b["datahora"]

    b = b[(b["inicio_gap"] < fim) & (b["fim_gap"] > inicio)].copy()

    ini_ov = b["inicio_gap"].clip(lower=inicio)
    fim_ov = b["fim_gap"].clip(upper=fim)
    b["min_na_janela"] = (fim_ov - ini_ov).dt.total_seconds() / 60
    return b


def silencios_iniciados(buracos, df, inicio, fim, min_horas=3):
    longos = buracos[buracos["gap"] >= pd.Timedelta(hours=min_horas)]
    na_janela = longos[(longos["inicio_gap"] >= inicio) & (longos["inicio_gap"] < fim)]
    fora = longos[(longos["inicio_gap"] < inicio) | (longos["inicio_gap"] >= fim)]

    horas_janela = (fim - inicio).total_seconds() / 3600
    horas_total = (df["datahora"].max() - df["datahora"].min()).total_seconds() / 3600
    horas_fora = horas_total - horas_janela

    return {
        "na_janela": len(na_janela),
        "fora": len(fora),
        "taxa_janela_por_h": len(na_janela) / horas_janela,
        "taxa_fora_por_h": len(fora) / horas_fora,
    }
import pandas as pd

import config


def estatisticas_chuva(df):
    s = df["valorMedida"]
    return {"media": s.mean(), "desvio": s.std(), "maximo": s.max(), "n": len(s)}


def chuva_por_hora(df):
    por_hora = df.groupby(df["datahora"].dt.floor("h"))["valorMedida"].sum()
    return por_hora.asfreq("h", fill_value=0)


def janela_critica(df, horas=None):
    if horas is None:
        horas = config.horas_janela
    soma_movel = chuva_por_hora(df).rolling(horas).sum()
    ultima = soma_movel.idxmax()
    inicio = ultima - pd.Timedelta(hours=horas - 1)
    fim = ultima + pd.Timedelta(hours=1)  # fim exclusivo
    return inicio, fim, soma_movel.max()


def chuva_na_janela(df, inicio, fim):
    na_janela = df[(df["datahora"] >= inicio) & (df["datahora"] < fim)]
    por_estacao = na_janela.groupby("codEstacao")["valorMedida"].sum()
    return {
        "soma": por_estacao.sum(),
        "estacoes": len(por_estacao),
        "media_por_estacao": por_estacao.mean(),
        "max_leitura": na_janela["valorMedida"].max(),
    }


def acumulado_por_dia(df, local=True):
    col = "datahora_local" if local else "datahora"
    return df.groupby(df[col].dt.date)["valorMedida"].sum().sort_values(ascending=False)
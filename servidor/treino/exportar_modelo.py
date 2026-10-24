"""Exporta o modelo de credibilidade treinado no scikit-learn para o JSON que
os nós em Go carregam (flag -modelo ou variável MODELO).

O modelo precisa ser um Pipeline(StandardScaler, LogisticRegression) treinado
com as colunas na ordem de FEATURES. As definições abaixo são as mesmas
calculadas em internal/credibilidade/credibilidade.go; se mudar uma, mude a outra.

    log_n_usuarios  ln(1 + nº de usuários distintos que relataram a até 1 km)
    chuva_max_mm_h  maior intensidade (mm/h) medida por estação a até 5 km,
                    de 1 h antes do primeiro relato até o fim da validade (6 h)
    dispersao_km    distância média dos relatos ao centro do alerta

Uso:
    from sklearn.pipeline import make_pipeline
    from sklearn.preprocessing import StandardScaler
    from sklearn.linear_model import LogisticRegression

    pipe = make_pipeline(StandardScaler(), LogisticRegression())
    pipe.fit(X[FEATURES], y)
    exportar(pipe, "../modelo/modelo.json", "lr-v1")
"""

import json

FEATURES = ["log_n_usuarios", "chuva_max_mm_h", "dispersao_km"]


def exportar(pipeline, caminho, versao):
    scaler = pipeline.named_steps["standardscaler"]
    lr = pipeline.named_steps["logisticregression"]
    modelo = {
        "versao": versao,
        "intercept": float(lr.intercept_[0]),
        "coef": dict(zip(FEATURES, map(float, lr.coef_[0]))),
        "media": dict(zip(FEATURES, map(float, scaler.mean_))),
        "escala": dict(zip(FEATURES, map(float, scaler.scale_))),
    }
    with open(caminho, "w", encoding="utf-8") as f:
        json.dump(modelo, f, indent=2)
    return modelo

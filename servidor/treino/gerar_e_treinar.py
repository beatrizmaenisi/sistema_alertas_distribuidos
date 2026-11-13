"""Gera o dataset sintético de alertas a partir das chuvas reais do CEMADEN,
treina o modelo de credibilidade e calcula as métricas do item 4.

Uso (a partir da pasta servidor/treino):
    pip install pandas numpy scikit-learn matplotlib "h3>=4"
    python gerar_e_treinar.py --raw ../../dados/raw --saida ../../resultados/ia

Saídas em --saida:
    alertas_sinteticos.csv   dataset (1 linha = 1 alerta) com rótulo
    metricas.json            todas as métricas
    matriz_confusao_teste.png
    modelo.json              pesos no formato lido pelos nós em Go (lr-v1)

Rótulo = definido pelo PROCESSO DE GERAÇÃO (verdadeiro/falso), não por observação
real de campo. As métricas refletem a simulação.
"""

import argparse
import json
import math
import os
from datetime import timezone

import numpy as np
import pandas as pd
from sklearn.ensemble import GradientBoostingClassifier, RandomForestClassifier
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import (accuracy_score, confusion_matrix, f1_score,
                             mean_absolute_error, mean_squared_error,
                             precision_score, recall_score, roc_auc_score)
from sklearn.model_selection import GroupKFold, cross_validate
from sklearn.pipeline import make_pipeline
from sklearn.preprocessing import StandardScaler
from sklearn.tree import DecisionTreeClassifier

from exportar_modelo import FEATURES, exportar

try:
    from celula import chave_alerta
    USA_H3 = True
except ImportError:  # sem h3: grade quadrada de ~0,0045 graus (só p/ teste)
    USA_H3 = False

    def chave_alerta(lat, lng, momento):
        brt = momento.astimezone(timezone.utc).timestamp() - 3 * 3600
        dia = pd.Timestamp(brt, unit="s").strftime("%Y%m%d")
        return f"g{round(lat / 0.0045)}_{round(lng / 0.0045)}_{dia}"

SEED = 42
RAIO_CHUVA_KM = 5.0
RAIO_USUARIOS_KM = 1.0
ARQUIVOS = {  # arquivo -> (cidade, conjunto)
    "RJ_Petropolis_2022-02.csv": ("Petropolis", "treino"),
    "RJ_Petropolis_2022-07.csv": ("Petropolis", "treino"),
    "BA_Salvador_2022-02.csv": ("Salvador", "teste"),
    "BA_Salvador_2022-10.csv": ("Salvador", "teste"),
}
N_POR_ARQUIVO = 500  # 4 arquivos -> 2.000 alertas, 50% verdadeiros / 50% falsos


def haversine_km(lat1, lon1, lat2, lon2):
    r = 6371.0088
    p1, p2 = np.radians(lat1), np.radians(lat2)
    dphi = p2 - p1
    dl = np.radians(lon2) - np.radians(lon1)
    a = np.sin(dphi / 2) ** 2 + np.cos(p1) * np.cos(p2) * np.sin(dl / 2) ** 2
    return 2 * r * np.arcsin(np.sqrt(a))


def ler_cemaden(caminho):
    df = pd.read_csv(caminho, sep=";", decimal=",", encoding="utf-8")
    df["datahora"] = pd.to_datetime(df["datahora"]).dt.tz_localize("UTC")
    # exclui estações com cobertura < 50% da mediana (mesma regra do pipeline)
    cont = df.groupby("codEstacao").size()
    ok = cont[cont >= 0.5 * cont.median()].index
    return df[df["codEstacao"].isin(ok)].copy()


def deslocar(lat, lng, dist_km, ang):
    dlat = dist_km * math.cos(ang) / 111.32
    dlng = dist_km * math.sin(ang) / (111.32 * math.cos(math.radians(lat)))
    return lat + dlat, lng + dlng


def atributos(rel_lat, rel_lng, t_primeiro, chuva):
    """Mesmas definições de internal/credibilidade (ver exportar_modelo.py)."""
    clat, clng = float(np.mean(rel_lat)), float(np.mean(rel_lng))
    dist = haversine_km(np.array(rel_lat), np.array(rel_lng), clat, clng)
    n_usuarios = int(np.sum(dist <= RAIO_USUARIOS_KM))
    dispersao = float(np.mean(dist))
    d = haversine_km(chuva["latitude"].values, chuva["longitude"].values, clat, clng)
    ini = t_primeiro - pd.Timedelta(hours=1)
    fim = t_primeiro + pd.Timedelta(hours=6)
    m = (d <= RAIO_CHUVA_KM) & (chuva["datahora"] >= ini) & (chuva["datahora"] <= fim)
    chuva_max = float(chuva.loc[m, "valorMedida"].max()) if m.any() else 0.0
    return clat, clng, math.log1p(n_usuarios), chuva_max, dispersao, n_usuarios


def gerar(arquivo, cidade, conjunto, chuva, rng, n):
    est = chuva.drop_duplicates("codEstacao")[["codEstacao", "latitude", "longitude"]]
    lat0, lat1 = est.latitude.min() - 0.02, est.latitude.max() + 0.02
    lng0, lng1 = est.longitude.min() - 0.02, est.longitude.max() + 0.02
    # horas com chuva relevante (candidatas a relatos verdadeiros)
    fortes = chuva[chuva["valorMedida"] >= 5.0]
    moderadas = chuva[(chuva["valorMedida"] >= 1.0) & (chuva["valorMedida"] < 5.0)]
    horas = sorted(chuva["datahora"].unique())
    linhas = []

    def montar(lat_c, lng_c, t0, n_users, sigma_km, rotulo, tipo):
        lats, lngs = [], []
        for _ in range(n_users):
            d = abs(rng.normal(0, sigma_km))
            la, ln = deslocar(lat_c, lng_c, d, rng.uniform(0, 2 * math.pi))
            lats.append(la)
            lngs.append(ln)
        clat, clng, lnu, cmax, disp, nu = atributos(lats, lngs, t0, chuva)
        linhas.append(dict(
            id_alerta=chave_alerta(clat, clng, t0.to_pydatetime()),
            cidade=cidade, conjunto=conjunto, arquivo=arquivo, tipo=tipo,
            lat=clat, lng=clng, t_primeiro_relato_utc=t0.isoformat(),
            n_relatos=n_users, n_usuarios_1km=nu,
            log_n_usuarios=lnu, chuva_max_mm_h=cmax, dispersao_km=disp,
            rotulo=rotulo))

    # ---- verdadeiros -------------------------------------------------
    for i in range(n // 2):
        u = rng.random()
        if u < 0.70:  # chuva forte perto de uma estação
            ref = fortes.iloc[rng.integers(len(fortes))]
            tipo = "verdadeiro_chuva_forte"
        else:  # caso difícil: chuva só moderada
            ref = moderadas.iloc[rng.integers(len(moderadas))]
            tipo = "verdadeiro_chuva_moderada"
        # local do relato: até 4 km da estação (alguns além do raio de 5 km pela dispersão)
        d = rng.uniform(0, 4.0)
        lat_c, lng_c = deslocar(ref["latitude"], ref["longitude"], d, rng.uniform(0, 2 * math.pi))
        t0 = ref["datahora"] + pd.Timedelta(minutes=int(rng.integers(0, 60)))
        # nº de usuários: 1 + Poisson(2) -> muitos casos com 1 ou 2 (difíceis)
        n_users = int(min(1 + rng.poisson(2.0), 10))
        montar(lat_c, lng_c, t0, n_users, rng.uniform(0.08, 0.5), 1, tipo)

    # ---- falsos ------------------------------------------------------
    for i in range(n - n // 2):
        u = rng.random()
        t0 = pd.Timestamp(horas[rng.integers(len(horas))]) + pd.Timedelta(minutes=int(rng.integers(0, 60)))
        lat_c = rng.uniform(lat0, lat1)
        lng_c = rng.uniform(lng0, lng1)
        if u < 0.45:  # relato isolado em ponto/horário aleatório
            tipo, n_users, sigma = "falso_isolado", int(rng.integers(1, 3)), rng.uniform(0.05, 0.4)
        elif u < 0.75:  # pequeno grupo disperso
            tipo, n_users, sigma = "falso_grupo_disperso", int(rng.integers(2, 6)), rng.uniform(0.5, 2.0)
        elif u < 0.90:  # grupo coordenado: vários usuários, bem juntos, sem chuva real
            tipo, n_users, sigma = "falso_coordenado", int(rng.integers(3, 8)), rng.uniform(0.03, 0.2)
        else:  # caso difícil: falso durante chuva forte (usuário exagera)
            ref = fortes.iloc[rng.integers(len(fortes))]
            lat_c, lng_c = deslocar(ref["latitude"], ref["longitude"], rng.uniform(0, 3.0), rng.uniform(0, 2 * math.pi))
            t0 = ref["datahora"] + pd.Timedelta(minutes=int(rng.integers(0, 60)))
            tipo, n_users, sigma = "falso_durante_chuva", int(rng.integers(1, 3)), rng.uniform(0.05, 0.4)
        montar(lat_c, lng_c, t0, n_users, sigma, 0, tipo)
    return linhas


def metricas(y, p, limiar=0.5):
    pred = (p >= limiar).astype(int)
    tn, fp, fn, tp = confusion_matrix(y, pred, labels=[0, 1]).ravel()
    return dict(
        limiar=limiar,
        acuracia=accuracy_score(y, pred),
        precisao=precision_score(y, pred, zero_division=0),
        recall=recall_score(y, pred, zero_division=0),
        f1=f1_score(y, pred, zero_division=0),
        roc_auc=roc_auc_score(y, p),
        rmse=math.sqrt(mean_squared_error(y, p)),
        mae=mean_absolute_error(y, p),
        matriz_confusao=dict(VN=int(tn), FP=int(fp), FN=int(fn), VP=int(tp)),
    )


def heuristico_v0(X):
    z = -2.0 + 1.6 * X["log_n_usuarios"] + 0.08 * X["chuva_max_mm_h"] - 0.5 * X["dispersao_km"]
    return 1 / (1 + np.exp(-z))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--raw", default="../../dados/raw")
    ap.add_argument("--saida", default="../../resultados/ia")
    ap.add_argument("--modelo", default="../modelo/modelo.json")
    a = ap.parse_args()
    os.makedirs(a.saida, exist_ok=True)
    os.makedirs(os.path.dirname(a.modelo), exist_ok=True)
    rng = np.random.default_rng(SEED)

    linhas = []
    for arq, (cidade, conj) in ARQUIVOS.items():
        chuva = ler_cemaden(os.path.join(a.raw, arq))
        linhas += gerar(arq, cidade, conj, chuva, rng, N_POR_ARQUIVO)
    df = pd.DataFrame(linhas)
    df.to_csv(os.path.join(a.saida, "alertas_sinteticos.csv"), index=False)

    tr, te = df[df.conjunto == "treino"], df[df.conjunto == "teste"]
    Xtr, ytr, Xte, yte = tr[FEATURES], tr["rotulo"], te[FEATURES], te["rotulo"]
    # grupo da validação cruzada: célula/grade grosseira (evita vizinhos em treino e validação)
    grupos = (tr["lat"].round(2).astype(str) + "_" + tr["lng"].round(2).astype(str))

    modelos = {
        "Regressao logistica": make_pipeline(StandardScaler(), LogisticRegression(C=1.0, max_iter=1000)),
        "Arvore de decisao": DecisionTreeClassifier(max_depth=4, random_state=SEED),
        "Random Forest": RandomForestClassifier(n_estimators=200, max_depth=6, random_state=SEED),
        "Gradient Boosting": GradientBoostingClassifier(random_state=SEED),
    }
    cv = GroupKFold(n_splits=5)
    resumo = {"n_total": len(df), "n_treino": len(tr), "n_teste": len(te),
              "balanceamento_treino": tr.rotulo.value_counts().to_dict(),
              "balanceamento_teste": te.rotulo.value_counts().to_dict(),
              "usa_h3": USA_H3, "comparacao": {}}
    for nome, m in modelos.items():
        r = cross_validate(m, Xtr, ytr, groups=grupos, cv=cv,
                           scoring=["accuracy", "precision", "recall", "f1", "roc_auc"])
        m.fit(Xtr, ytr)
        p = m.predict_proba(Xte)[:, 1]
        resumo["comparacao"][nome] = {
            "cv5_" + k[5:]: [float(np.mean(v)), float(np.std(v))]
            for k, v in r.items() if k.startswith("test_")}
        resumo["comparacao"][nome]["teste"] = metricas(yte, p)

    lr = modelos["Regressao logistica"]
    p_te = lr.predict_proba(Xte)[:, 1]
    resumo["modelo_escolhido"] = "Regressao logistica"
    resumo["teste_por_limiar"] = {str(l): metricas(yte, p_te, l) for l in (0.4, 0.5, 0.7)}
    resumo["teste_por_cidade_arquivo"] = {
        arq: metricas(g["rotulo"], lr.predict_proba(g[FEATURES])[:, 1])
        for arq, g in te.groupby("arquivo")}
    resumo["baseline_heuristico_v0_teste"] = metricas(yte, heuristico_v0(Xte).values)
    # níveis de alerta no teste
    nivel = pd.cut(p_te, [-1, 0.4, 0.7, 2], labels=["INFO", "ATENCAO", "EMERGENCIA"])
    resumo["niveis_teste"] = pd.crosstab(nivel, yte.values).to_dict()
    lrm = lr.named_steps["logisticregression"]
    resumo["coeficientes_padronizados"] = dict(zip(FEATURES, map(float, lrm.coef_[0])))
    resumo["intercepto"] = float(lrm.intercept_[0])

    modelo = exportar(lr, a.modelo, "lr-v1")
    resumo["modelo_json"] = modelo
    with open(os.path.join(a.saida, "metricas.json"), "w", encoding="utf-8") as f:
        json.dump(resumo, f, indent=2, ensure_ascii=False, default=float)
    with open(os.path.join(a.saida, "modelo.json"), "w", encoding="utf-8") as f:
        json.dump(modelo, f, indent=2)

    # matriz de confusão (teste, limiar 0,5)
    import matplotlib
    matplotlib.use("Agg")
    import matplotlib.pyplot as plt
    mc = resumo["teste_por_limiar"]["0.5"]["matriz_confusao"]
    cm = np.array([[mc["VN"], mc["FP"]], [mc["FN"], mc["VP"]]])
    fig, ax = plt.subplots(figsize=(4.8, 4.2))
    ax.imshow(cm, cmap="Blues")
    for i in range(2):
        for j in range(2):
            ax.text(j, i, cm[i, j], ha="center", va="center", fontsize=16,
                    color="white" if cm[i, j] > cm.max() / 2 else "black")
    ax.set_xticks([0, 1], ["Falso", "Verdadeiro"])
    ax.set_yticks([0, 1], ["Falso", "Verdadeiro"])
    ax.set_xlabel("Previsto")
    ax.set_ylabel("Real (rótulo sintético)")
    ax.set_title("Matriz de confusão: teste (Salvador), limiar 0,5")
    fig.tight_layout()
    fig.savefig(os.path.join(a.saida, "matriz_confusao_teste.png"), dpi=200)

    print(json.dumps({k: resumo[k] for k in
          ["n_total", "n_treino", "n_teste", "usa_h3", "coeficientes_padronizados", "intercepto"]}, indent=2))
    for nome, v in resumo["comparacao"].items():
        t = v["teste"]
        print(f"{nome:22s} CV f1={v['cv5_f1'][0]:.3f}±{v['cv5_f1'][1]:.3f} | teste acc={t['acuracia']:.3f} "
              f"prec={t['precisao']:.3f} rec={t['recall']:.3f} f1={t['f1']:.3f} auc={t['roc_auc']:.3f}")
    b = resumo["baseline_heuristico_v0_teste"]
    print(f"{'heuristico-v0':22s} teste acc={b['acuracia']:.3f} prec={b['precisao']:.3f} rec={b['recall']:.3f} f1={b['f1']:.3f}")
    print("matriz teste 0,5:", mc)


if __name__ == "__main__":
    main()
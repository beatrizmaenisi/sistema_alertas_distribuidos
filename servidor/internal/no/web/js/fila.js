const abrir = () => new Promise((ok, falha) => {
  const pedido = indexedDB.open('alertas', 1);
  pedido.onupgradeneeded = () => pedido.result.createObjectStore('relatos', { keyPath: 'id', autoIncrement: true });
  pedido.onsuccess = () => ok(pedido.result);
  pedido.onerror = () => falha(pedido.error);
});

async function operar(modo, operacao) {
  const banco = await abrir();
  return new Promise((ok, falha) => {
    const transacao = banco.transaction('relatos', modo);
    const pedido = operacao(transacao.objectStore('relatos'));
    transacao.oncomplete = () => { banco.close(); ok(pedido.result); };
    transacao.onabort = () => { banco.close(); falha(transacao.error); };
  });
}

export const guardar = relato => operar('readwrite', relatos => relatos.add(relato));
export const remover = id => operar('readwrite', relatos => relatos.delete(id));
export const todos = () => operar('readonly', relatos => relatos.getAll());

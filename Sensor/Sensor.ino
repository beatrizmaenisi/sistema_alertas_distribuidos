
#include <DHT.h>
#include <WiFi.h>
#include <WiFiClientSecure.h>
#include <ArduinoJson.h>
#include <HTTPClient.h>
#define DHTPIN 4
#define DHTTYPE DHT22

const char* ssid = "Wifi";
const char* password = "Wifi_Senha";
const char* server = "Link/Setvidor";
const char* apikey = "Chave";

DHT dht(DHTPIN, DHTTYPE);

bool enviado = false;
void Wifi(){
  int passos = 0;
  Serial.print("Conectando a internet ");

  WiFi.mode(WIFI_STA);
  WiFi.begin(ssid, password);

  while(WiFi.status() != WL_CONNECTED && passos < 30){
    delay(500);
    Serial.print(".");
    passos++;
  }
  if (WiFi.status() == WL_CONNECTED){
    Serial.println("\nRede conectada.");
    Serial.print("IP: ");
    Serial.print(WiFi.localIP());
  }
  else{
    Serial.println("Não foi possivel se conectar a rede");
  }
}

void enviarDados(String jsonPayload){
  if(WiFi.status() != WL_CONNECTED){
    Serial.println("Sem Conexão, não foi possível enviar");
    return;
  }
   WiFiClientSecure client;
  client.setInsecure();

  HTTPClient http;
  if (http.begin(client, server)) {
    http.addHeader("Content-Type", "application/json");
    http.addHeader("x-api-key", apikey);

    int httpResponseCode = http.POST(jsonPayload);

    if (httpResponseCode > 0) {
      Serial.print("Enviado com sucesso! Codigo HTTP: ");
      Serial.println(httpResponseCode);
      String resposta = http.getString();
      Serial.println("Resposta do servidor: " + resposta);
    } else {
      Serial.print("Erro no envio HTTP: ");
      Serial.println(http.errorToString(httpResponseCode));
    }

    http.end();
  } else {
    Serial.println("Falha ao iniciar conexao HTTPS com o servidor.");
  }
}
void setup() {
  Serial.begin(115200);
  dht.begin();
  Wifi();
}

void loop() {
  if(WiFi.status() != WL_CONNECTED){
    Wifi();
  }
  delay(5000);
  float umidade = dht.readHumidity();
  float temperatura = dht.readTemperature();

  if (isnan(umidade) || isnan(temperatura)) {
    Serial.println("{\"erro\": \"Falha ao ler o sensor\"}");
    return;
  }

  JsonDocument doc;
  doc["sensor"] = "DHT22_Base";
  doc["temperatura"] = round(temperatura * 10) / 10.0;
  doc["umidade"] = round(umidade * 10) / 10.0;

  String jsonString;
  serializeJson(doc, jsonString);
  Serial.println(jsonString);
  if (!enviado){
     enviarDados(jsonString);
     enviado=true;
  }
  else{
    Serial.println("Envio bloqueado com sucesso");
  }
}

#include <WiFi.h>

// Replace with your network credentials
const char* ssid     = "OVI-MK1";
const char* password = "";

// Set web server port number to 80
WiFiServer server(80);

// Variable to store the HTTP request
String header;

// Auxiliar variables to store the current output state
String output26State = "off";
String output27State = "off";

// Setup
void setup() {
  Serial.begin(115200);

  // Connect to Wi-Fi network with SSID and password
  Serial.print("Setting AP (Access Point)…");
  // Remove the password parameter, if you want the AP (Access Point) to be open
  WiFi.softAP(ssid, password);

  IPAddress IP = WiFi.softAPIP();
  Serial.print("AP IP address: ");
  Serial.println(IP);
  
  server.begin();


  // Init pins
  pinMode(4, OUTPUT);
  pinMode(5, OUTPUT);
  pinMode(6, OUTPUT);
  pinMode(7, OUTPUT);
  pinMode(15, OUTPUT);
  pinMode(16, OUTPUT);
  pinMode(17, OUTPUT);
}

void loop(){
  WiFiClient client = server.available();   // Listen for incoming clients

  if (client) {
    String currentLine = "";
    while (client.connected()) {
      if (client.available()) {
        char c = client.read();
        header += c;
        if (c == '\n') {
          if (currentLine.length() == 0) {
            client.println("HTTP/1.1 200 OK");
            break;
          } else {
            currentLine = "";
          }
        } else if (c != '\r') {
          currentLine += c;
        }
      }
      handleRequest(header);
    }
    // Clear the header variable
    header = "";
    // Close the connection
    client.stop();
    Serial.println("Client disconnected.");
    Serial.println("");
  }
}

struct Data {
  String type;
  String value;

};

struct Data compiledData[20];

void handleRequest(String header) {
  // Make sure we got a POST
  if (header.substring(0, 4) != "POST") {
    Serial.println("Not a POST request!");
    return;
  }

  // Delete first line
  for (int i=0; i<header.length(); i++) {
    String c = header.substring(i, i+1);

    if (c == "\n"){
      header = header.substring(i+1, header.length());
      break;
    }
  }

  // Read data
  bool readingValue = false;
  int lineIndex=0, startIndex=0;
  String buff;
  for (int i=0; i<header.length(); i++) {
    String c = header.substring(i, i+1);
    if (c == "\n") {
      compiledData[lineIndex].value = buff.substring(1);
      buff = "";
      lineIndex++;
    } else if (c == ":") {
      compiledData[lineIndex].type = buff;
      buff = "";
    } else {
      buff += c;
    }
  }

  // Interpret data
  for (int i=0; i<lineIndex; i++) {
    if (compiledData[i].type == "R1") {
      analogWrite(4, compiledData[i].value.toInt());

    } else if (compiledData[i].type == "U1") {
      analogWrite(5, compiledData[i].value.toInt());
    
    } else if (compiledData[i].type == "E1") {
      analogWrite(6, compiledData[i].value.toInt());
    
    } else if (compiledData[i].type == "R2") {
      analogWrite(7, compiledData[i].value.toInt());
    
    } else if (compiledData[i].type == "U2") {
      analogWrite(15, compiledData[i].value.toInt());
    
    } else if (compiledData[i].type == "E2") {
      analogWrite(16, compiledData[i].value.toInt());
    
    } else if (compiledData[i].type == "G1") {
      analogWrite(17, compiledData[i].value.toInt());
    
    }
  }
  

  
}

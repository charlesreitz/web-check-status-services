package main

import (
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"gopkg.in/ini.v1"
)

type Service struct {
	id          int // Usar o ID como número da linha
	Description string
	IP          string
	Port        string
	Status      string
}

var services []Service
var previousServices []Service
var upgrader = websocket.Upgrader{}
var serverPort string
var responseTime int // Variável para armazenar o tempo de resposta

// Função para carregar o arquivo de configuração e iniciar o monitoramento
func loadConfig(filename string) ([]Service, string, int, error) {
	cfg, err := ini.Load(filename)
	if err != nil {
		return nil, "", 0, err
	}

	// Lendo a porta do servidor
	port := cfg.Section("general").Key("port").String()

	// Lendo o tempo de resposta
	responseTime, err := strconv.Atoi(cfg.Section("general").Key("response_time").String())
	if err != nil {
		log.Println("Erro ao converter response_time, usando valor padrão de 10 segundos")
		responseTime = 10
	}

	// Lendo a seção de serviços
	services := []Service{}
	serviceSection := cfg.Section("services")
	for i, key := range serviceSection.Keys() {
		serviceData := strings.Split(key.Value(), ":")
		if len(serviceData) == 2 {
			services = append(services, Service{
				id:          i + 1, // Atribuindo o número da linha como ID
				Description: key.Name(),
				IP:          serviceData[0],
				Port:        serviceData[1],
				Status:      "unknown", // Status inicial desconhecido
			})
		}
	}

	return services, port, responseTime, nil
}

// Função para verificar o status de um serviço (online ou offline)
func checkService(ip, port string) string {
	timeout := time.Second * 2
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, port), timeout)

	if err != nil {
		// Se houver erro, retornamos "red" como offline
		log.Printf("Erro ao verificar serviço %s:%s - %v", ip, port, err)
		return "red"
	}
	defer conn.Close()

	// Retorna "green" se o serviço está online
	return "green"
}

// Função para comparar estados dos serviços e retornar true se houver alterações
func hasChanges(currentServices, previousServices []Service) bool {
	if len(currentServices) != len(previousServices) {
		return true
	}
	for i := range currentServices {
		if currentServices[i].Status != previousServices[i].Status {
			return true
		}
	}
	return false
}

// Função para monitorar os serviços e enviar atualizações apenas quando houver alterações
func monitorServices(services []Service, conn *websocket.Conn) {
	// Copia o estado inicial para comparar mais tarde
	previousServices = make([]Service, len(services))
	copy(previousServices, services)

	for {
		var changes []Service
		for i := range services {
			// Verifica o status atual do serviço
			currentStatus := checkService(services[i].IP, services[i].Port)

			// Atualiza o status apenas se houve mudanças
			if currentStatus != services[i].Status {
				services[i].Status = currentStatus
				changes = append(changes, services[i])
			}
		}

		// Compara o estado atual com o estado anterior para ver se houve mudanças
		if hasChanges(services, previousServices) {
			// Se houver alterações, envia os dados para o front-end via WebSocket
			if err := conn.WriteJSON(services); err != nil {
				log.Println("Erro ao enviar atualizações:", err)
				break
			}
			fmt.Println("Alterações detectadas e enviadas:", services)
			// Atualiza o estado anterior com o novo estado
			copy(previousServices, services)
		}

		// Espera antes de realizar a próxima verificação
		time.Sleep(time.Duration(responseTime) * time.Second)
	}
}

// WebSocket handler para enviar dados para o front-end
func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Erro ao abrir WebSocket:", err)
		return
	}
	defer conn.Close()

	// Começa a monitorar os serviços e envia atualizações apenas quando houver alterações
	monitorServices(services, conn)
}

// Handler para a página inicial
func indexHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

func main() {
	// Inicialmente carregar a configuração
	var err error
	services, serverPort, responseTime, err = loadConfig("config.ini")
	if err != nil {
		log.Fatal("Erro ao carregar arquivo de configuração:", err)
	}

	// Iniciar o servidor na porta definida no arquivo .ini
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/", indexHandler)
	log.Printf("Servidor iniciado na porta :%s\n", serverPort)
	log.Fatal(http.ListenAndServe(":"+serverPort, nil))
}

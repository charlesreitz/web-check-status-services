package main

import (
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"gopkg.in/ini.v1"
)

type Service struct {
	id           int    // Usar o ID como número da linha
	Description  string // Descrição do serviço
	IP           string // IP do serviço
	Port         string // Porta do serviço
	Status       string // Status atual do serviço
	ResponseTime string // Tempo de resposta em milissegundos (string para facilitar exibição no front-end)
}

var services []Service
var latestServicesState []Service // Variável global para armazenar o último estado dos serviços
var upgrader = websocket.Upgrader{}
var serverPort string
var responseTime int          // Variável para armazenar o tempo de resposta
var mu sync.Mutex             // Mutex para proteger o acesso concorrente à variável latestServicesState
var configFile = "config.ini" // Nome do arquivo de configuração
var lastModTime time.Time     // Armazenará a última modificação do arquivo config.ini

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

// Função para verificar o status de um serviço (online ou offline) e calcular o tempo de resposta
func checkService(description, ip, port string) (string, string) {
	start := time.Now() // Início do cálculo do tempo de resposta
	timeout := time.Second
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, port), timeout)
	responseTime := time.Since(start).Milliseconds() // Calcula o tempo de resposta em milissegundos

	if err != nil {
		// Se houver erro, retornamos "red" como offline e incluímos a descrição do serviço no log
		log.Printf("Erro ao verificar serviço [%s] %s:%s - %v", description, ip, port, err)
		return "red", strconv.FormatInt(responseTime, 10) + " ms"
	}
	defer conn.Close()

	// Retorna "green" se o serviço está online
	log.Printf("Serviço [%s] %s:%s está online. Tempo de resposta: %d ms", description, ip, port, responseTime)
	return "green", strconv.FormatInt(responseTime, 10) + " ms"
}

func monitorServices(services *[]Service) {
	for {
		for i := range *services {
			// Verifica se o arquivo de configuração foi alterado durante a execução
			if hasConfigFileChanged() {
				log.Println("Arquivo config.ini modificado, recarregando configurações...")
				restartServices(services) // Passa o ponteiro de services para a função
				break
			}

			// Verifica o status atual do serviço e calcula o tempo de resposta
			currentStatus, responseTime := checkService((*services)[i].Description, (*services)[i].IP, (*services)[i].Port)

			// Atualiza o status e tempo de resposta apenas se houver mudanças
			if currentStatus != (*services)[i].Status || responseTime != (*services)[i].ResponseTime {
				(*services)[i].Status = currentStatus
				(*services)[i].ResponseTime = responseTime
			}

			// Atualiza o último estado dos serviços na variável global
			mu.Lock()
			latestServicesState[i] = (*services)[i]
			mu.Unlock()
		}

		// Espera antes de realizar a próxima verificação
		time.Sleep(time.Duration(responseTime) * time.Second)
	}
}

func hasConfigFileChanged() bool {
	info, err := os.Stat(configFile)
	if err != nil {
		log.Println("Erro ao verificar arquivo de configuração:", err)
		return false
	}

	modTime := info.ModTime()
	if modTime.After(lastModTime) {
		lastModTime = modTime // Atualiza o tempo de modificação
		return true           // Retorna verdadeiro se o arquivo foi modificado
	}
	return false
}

// / Função para reiniciar os serviços após a alteração no arquivo config.ini
func restartServices(services *[]Service) {
	mu.Lock()
	defer mu.Unlock()

	// Recarregar as configurações
	var err error
	*services, serverPort, responseTime, err = loadConfig(configFile)
	if err != nil {
		log.Fatalf("Erro ao recarregar arquivo de configuração: %v", err)
	}

	// Atualiza o último tempo de modificação
	info, _ := os.Stat(configFile)
	lastModTime = info.ModTime()

	latestServicesState = make([]Service, len(*services))
	copy(latestServicesState, *services)

	log.Println("Configurações recarregadas com sucesso!")
}

// WebSocket handler para enviar dados para o front-end
func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Erro ao abrir WebSocket:", err)
		return
	}
	defer conn.Close()

	// Envia o último estado dos serviços armazenado em memória inicialmente
	mu.Lock()
	if len(latestServicesState) > 0 {
		log.Println("Enviando último estado armazenado para o WebSocket:", latestServicesState)
		if err := conn.WriteJSON(latestServicesState); err != nil {
			log.Println("Erro ao enviar último estado:", err)
			mu.Unlock()
			return
		}
	}
	mu.Unlock()

	// Continua enviando atualizações periódicas conforme o intervalo definido no config.ini
	ticker := time.NewTicker(time.Duration(responseTime) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mu.Lock()
			// Envia o último estado dos serviços armazenado em memória
			if len(latestServicesState) > 0 {
				log.Println("Enviando atualizações periódicas para o WebSocket:", latestServicesState)
				if err := conn.WriteJSON(latestServicesState); err != nil {
					log.Println("Erro ao enviar atualizações periódicas:", err)
					mu.Unlock()
					return
				}
			}
			mu.Unlock()
		case <-r.Context().Done():
			// O WebSocket foi fechado
			log.Println("Conexão WebSocket fechada.")
			return
		}
	}
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
	// Carregar a configuração inicialmente
	var err error
	services, serverPort, responseTime, err = loadConfig(configFile)
	if err != nil {
		log.Fatal("Erro ao carregar arquivo de configuração:", err)
	}

	// Inicializa o estado mais recente dos serviços em memória
	latestServicesState = make([]Service, len(services))
	copy(latestServicesState, services)

	// Armazena o tempo de modificação inicial do arquivo config.ini
	info, _ := os.Stat(configFile)
	lastModTime = info.ModTime()

	// Iniciar o monitoramento dos serviços em uma goroutine
	go monitorServices(&services) // Passa o ponteiro de services para o monitoramento

	// Iniciar o servidor na porta definida no arquivo .ini
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/", indexHandler)
	log.Printf("Servidor iniciado na porta :%s\n", serverPort)
	log.Fatal(http.ListenAndServe(":"+serverPort, nil))
}

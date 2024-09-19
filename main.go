package main

import (
	"html/template"
	"io" // Import adicionado
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"gopkg.in/ini.v1"
)

type Service struct {
	ID           int    `json:"id"`           // Exportado e incluído no JSON
	Description  string `json:"Description"`  // Exportado e incluído no JSON
	IP           string `json:"-"`            // Excluído do JSON
	Port         string `json:"-"`            // Excluído do JSON
	Status       string `json:"Status"`       // Exportado e incluído no JSON
	ResponseTime string `json:"ResponseTime"` // Exportado e incluído no JSON
}

var services []Service
var latestServicesState []Service // Variável global para armazenar o último estado dos serviços
var upgrader = websocket.Upgrader{}
var serverPort string
var responseTime int          // Variável para armazenar o tempo de resposta
var mu sync.Mutex             // Mutex para proteger o acesso concorrente à variável latestServicesState
var configFile = "config.ini" // Nome do arquivo de configuração
var lastModTime time.Time     // Armazenará a última modificação do arquivo config.ini
var pathLog string

// Função para carregar o arquivo de configuração e iniciar o monitoramento
func loadConfig(filename string) ([]Service, string, int, string, error) {
	cfg, err := ini.Load(filename)
	if err != nil {
		return nil, "", 0, "", err
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
				ID:          i + 1, // Atribuindo o número da linha como ID
				Description: key.Name(),
				IP:          serviceData[0],
				Port:        serviceData[1],
				Status:      "unknown", // Status inicial desconhecido
			})
		}
	}
	pathLog := cfg.Section("general").Key("pathlog").String()
	return services, port, responseTime, pathLog, nil
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

// Função para reiniciar os serviços após a alteração no arquivo config.ini
func restartServices(services *[]Service) {
	mu.Lock()
	defer mu.Unlock()

	// Recarregar as configurações
	var err error
	*services, serverPort, responseTime, pathLog, err = loadConfig(configFile)
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
		log.Println("Enviando último estado armazenado para o WebSocket")
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
				log.Println("Enviando atualizações periódicas para o WebSocket")
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

// Função para criar um arquivo de log diário e também imprimir no console
func setupLog(pathLog string) {
	currentTime := time.Now().Format("2006-01-02")
	logDir := pathLog //"./logs"

	// Verifica se o diretório de logs existe, senão, cria
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		err := os.MkdirAll(logDir, 0755) // Use MkdirAll para criar diretórios pai, se necessário
		if err != nil {
			log.Fatalf("Erro ao criar diretório de logs: %v", err)
		}
	}

	logFile := filepath.Join(logDir, currentTime+".log")
	file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Erro ao abrir arquivo de log: %v", err)
	}

	// Cria um MultiWriter para escrever tanto no arquivo quanto no console
	mw := io.MultiWriter(os.Stdout, file)
	log.SetOutput(mw)

	// Configura o prefixo e o formato dos logs (opcional, mas recomendado)
	log.SetPrefix("[" + currentTime + "] ")
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// Limpar logs antigos
	cleanupOldLogs(logDir, 10) // Mantém apenas os últimos 10 dias
}

// Função para remover logs mais antigos que um certo número de dias
func cleanupOldLogs(logDir string, maxDays int) {
	files, err := os.ReadDir(logDir)
	if err != nil {
		log.Println("Erro ao ler diretório de logs:", err)
		return
	}

	threshold := time.Now().AddDate(0, 0, -maxDays) // Data limite para remoção

	for _, file := range files {
		filePath := filepath.Join(logDir, file.Name())
		info, err := os.Stat(filePath)
		if err != nil {
			log.Println("Erro ao obter informações do arquivo:", file.Name())
			continue
		}

		// Remove arquivos mais antigos que a data limite
		if info.ModTime().Before(threshold) {
			if err := os.Remove(filePath); err != nil {
				log.Println("Erro ao remover arquivo:", file.Name())
			} else {
				log.Println("Arquivo removido:", file.Name())
			}
		}
	}
}

func main() {

	// Carregar a configuração inicialmente
	var err error
	services, serverPort, responseTime, pathLog, err = loadConfig(configFile)
	if err != nil {
		log.Fatal("Erro ao carregar arquivo de configuração:", err)
	}

	// Configurar logs diários
	setupLog(pathLog)

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

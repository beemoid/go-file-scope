package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Config struct {
	Port   string
	DBPath string
}

// Request structure from .bat script
type FileReport struct {
	HostName    string      `json:"host_name"`
	HostIP      string      `json:"host_ip"`
	Timestamp   string      `json:"timestamp"`
	BasePath    string      `json:"base_path"`
	Directories []Directory `json:"directories"`
	Totals      Totals      `json:"totals"`
}

type Directory struct {
	Path      string `json:"path"`
	FileCount int    `json:"file_count"`
	SizeBytes int64  `json:"size_bytes"`
	SizeMB    int    `json:"size_mb"`
}

type Totals struct {
	TotalDirectories int   `json:"total_directories"`
	TotalFiles       int   `json:"total_files"`
	TotalSizeBytes   int64 `json:"total_size_bytes"`
	TotalSizeMB      int   `json:"total_size_mb"`
	TotalSizeGB      int   `json:"total_size_gb"`
}

// Database models
type HostSummary struct {
	HostName    string    `json:"host_name"`
	HostIP      string    `json:"host_ip"`
	LastReport  time.Time `json:"last_report"`
	TotalFiles  int       `json:"total_files"`
	TotalSizeMB int       `json:"total_size_mb"`
	TotalSizeGB int       `json:"total_size_gb"`
	ReportCount int       `json:"report_count"`
}

var db *sql.DB
var config Config

// Initialize database
func initDB(dbPath string) error {
	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return err
	}

	// Create reports table
	createReportsTable := `CREATE TABLE IF NOT EXISTS file_reports (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_name TEXT NOT NULL,
		host_ip TEXT NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		base_path TEXT,
		total_directories INTEGER,
		total_files INTEGER,
		total_size_bytes INTEGER,
		total_size_mb INTEGER,
		total_size_gb INTEGER,
		report_data TEXT
	);`

	_, err = db.Exec(createReportsTable)
	if err != nil {
		return err
	}

	// Create directories table
	createDirectoriesTable := `CREATE TABLE IF NOT EXISTS directory_details (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		report_id INTEGER,
		path TEXT,
		file_count INTEGER,
		size_bytes INTEGER,
		size_mb INTEGER,
		FOREIGN KEY(report_id) REFERENCES file_reports(id)
	);`

	_, err = db.Exec(createDirectoriesTable)
	if err != nil {
		return err
	}

	// Create index for faster queries
	db.Exec("CREATE INDEX IF NOT EXISTS idx_host_ip ON file_reports(host_ip);")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_timestamp ON file_reports(timestamp);")

	log.Println("Database initialized successfully")
	return nil
}

// Save file report to database
func saveFileReport(report FileReport) (int64, error) {
	// Convert report to JSON for storage
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return 0, err
	}

	// Insert main report
	insertSQL := `INSERT INTO file_reports 
		(host_name, host_ip, base_path, total_directories, total_files, 
		total_size_bytes, total_size_mb, total_size_gb, report_data)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	result, err := db.Exec(insertSQL,
		report.HostName,
		report.HostIP,
		report.BasePath,
		report.Totals.TotalDirectories,
		report.Totals.TotalFiles,
		report.Totals.TotalSizeBytes,
		report.Totals.TotalSizeMB,
		report.Totals.TotalSizeGB,
		string(reportJSON))

	if err != nil {
		return 0, err
	}

	reportID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	// Insert directory details
	for _, dir := range report.Directories {
		_, err = db.Exec(`INSERT INTO directory_details 
			(report_id, path, file_count, size_bytes, size_mb)
			VALUES (?, ?, ?, ?, ?)`,
			reportID, dir.Path, dir.FileCount, dir.SizeBytes, dir.SizeMB)
		if err != nil {
			log.Printf("Warning: Failed to insert directory detail: %v", err)
		}
	}

	return reportID, nil
}

// Handler for receiving file reports
func commandHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse JSON
	var report FileReport
	err = json.Unmarshal(body, &report)
	if err != nil {
		log.Printf("JSON parse error: %v", err)
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	// Save to database
	reportID, err := saveFileReport(report)
	if err != nil {
		log.Printf("Database error: %v", err)
		http.Error(w, "Failed to save report", http.StatusInternalServerError)
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	log.Printf("[%s] Report received from %s (%s) - ID: %d - Files: %d, Size: %d MB",
		timestamp, report.HostName, report.HostIP, reportID,
		report.Totals.TotalFiles, report.Totals.TotalSizeMB)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "success",
		"report_id": reportID,
		"message":   "File report saved successfully",
	})
}

// Debug endpoint to check raw data
func debugHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, host_name, host_ip, total_files, total_size_mb FROM file_reports`)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var id int
		var hostName, hostIP string
		var totalFiles, totalSizeMB int
		err := rows.Scan(&id, &hostName, &hostIP, &totalFiles, &totalSizeMB)
		if err != nil {
			continue
		}
		results = append(results, map[string]interface{}{
			"id":            id,
			"host_name":     hostName,
			"host_ip":       hostIP,
			"total_files":   totalFiles,
			"total_size_mb": totalSizeMB,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_rows": len(results),
		"data":       results,
	})
}

// Get all hosts summary
func getHostsHandler(w http.ResponseWriter, r *http.Request) {
	query := `
		SELECT 
			host_name,
			host_ip,
			timestamp as last_report,
			total_files,
			total_size_mb,
			total_size_gb,
			COUNT(*) OVER (PARTITION BY host_ip) as report_count
		FROM file_reports
		WHERE (host_ip, timestamp) IN (
			SELECT host_ip, MAX(timestamp) FROM file_reports GROUP BY host_ip
		)
		ORDER BY timestamp DESC
	`

	rows, err := db.Query(query)
	if err != nil {
		log.Printf("Database query error: %v, Query: %s", err, query)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	hosts := make([]HostSummary, 0)
	for rows.Next() {
		var host HostSummary
		err := rows.Scan(&host.HostName, &host.HostIP, &host.LastReport,
			&host.TotalFiles, &host.TotalSizeMB, &host.TotalSizeGB, &host.ReportCount)
		if err != nil {
			log.Printf("Row scan error: %v", err)
			continue
		}
		hosts = append(hosts, host)
	}

	log.Printf("Returning %d hosts from API", len(hosts))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(hosts)
}

// Get reports for specific host
func getHostReportsHandler(w http.ResponseWriter, r *http.Request) {
	hostIP := r.URL.Query().Get("ip")
	if hostIP == "" {
		http.Error(w, "IP parameter required", http.StatusBadRequest)
		return
	}

	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "50"
	}

	rows, err := db.Query(`
		SELECT id, host_name, host_ip, timestamp, total_files, 
			   total_size_mb, total_size_gb, report_data
		FROM file_reports
		WHERE host_ip = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`, hostIP, limit)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var reports []map[string]interface{}
	for rows.Next() {
		var id int
		var hostName, hostIP, reportData string
		var timestamp time.Time
		var totalFiles, totalSizeMB, totalSizeGB int

		err := rows.Scan(&id, &hostName, &hostIP, &timestamp,
			&totalFiles, &totalSizeMB, &totalSizeGB, &reportData)
		if err != nil {
			continue
		}

		reports = append(reports, map[string]interface{}{
			"id":            id,
			"host_name":     hostName,
			"host_ip":       hostIP,
			"timestamp":     timestamp,
			"total_files":   totalFiles,
			"total_size_mb": totalSizeMB,
			"total_size_gb": totalSizeGB,
			"report_data":   reportData,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reports)
}

// Get directory details for a report
func getReportDetailsHandler(w http.ResponseWriter, r *http.Request) {
	reportID := r.URL.Query().Get("id")
	if reportID == "" {
		http.Error(w, "ID parameter required", http.StatusBadRequest)
		return
	}

	// Get report info
	var reportData string
	err := db.QueryRow("SELECT report_data FROM file_reports WHERE id = ?", reportID).Scan(&reportData)
	if err != nil {
		http.Error(w, "Report not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(reportData))
}

// Middleware to add CORS headers
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Dashboard handler - serves the HTML file
func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	// Serve the dashboard.html file
	htmlContent, err := os.ReadFile("dashboard.html")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Dashboard file not found. Place dashboard.html in the same directory as the executable.",
			"error":   err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(htmlContent)
}

// Health check
func healthHandler(w http.ResponseWriter, r *http.Request) {
	err := db.Ping()
	dbStatus := "healthy"
	if err != nil {
		dbStatus = "unhealthy: " + err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "healthy",
		"time":     time.Now().Format(time.RFC3339),
		"database": dbStatus,
	})
}

func main() {
	config.Port = "5555"
	config.DBPath = "file_reports.db"

	err := initDB(config.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Create a router with CORS middleware
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/command", commandHandler)
	mux.HandleFunc("/api/hosts", getHostsHandler)
	mux.HandleFunc("/api/host/reports", getHostReportsHandler)
	mux.HandleFunc("/api/report/details", getReportDetailsHandler)
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/debug", debugHandler)

	// Dashboard
	mux.HandleFunc("/", dashboardHandler)

	// Wrap with CORS middleware
	handler := corsMiddleware(mux)

	fmt.Printf("╔════════════════════════════════════════╗\n")
	fmt.Printf("║   File Report Server Started!          ║\n")
	fmt.Printf("╚════════════════════════════════════════╝\n\n")
	fmt.Printf("Server Port: %s\n", config.Port)
	fmt.Printf("Database: %s\n\n", config.DBPath)
	fmt.Printf("Endpoints:\n")
	fmt.Printf("  Dashboard:  http://localhost:%s/\n", config.Port)
	fmt.Printf("  API:        http://localhost:%s/command\n", config.Port)
	fmt.Printf("  Health:     http://localhost:%s/health\n\n", config.Port)

	log.Fatal(http.ListenAndServe(":"+config.Port, handler))
}

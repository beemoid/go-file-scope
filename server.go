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

// Request structure from .bat script - now supports dynamic fields
type FileReport struct {
	HostIP           string      `json:"host_ip"`
	Timestamp        string      `json:"timestamp"`
	BasePath         string      `json:"base_path"`
	Directories      []Directory `json:"directories"`
	TotalDirectories int         `json:"total_directories"`
}

type Directory struct {
	Path      string `json:"path"`
	FileCount int    `json:"file_count"`
	SizeBytes int64  `json:"size_bytes"`
	SizeMB    int    `json:"size_mb"`
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
		host_ip TEXT NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		base_path TEXT,
		total_directories INTEGER,
		report_data TEXT,
		UNIQUE(host_ip, timestamp)
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

	// Create audit log table
	createAuditLog := `CREATE TABLE IF NOT EXISTS audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		host_ip TEXT,
		action TEXT,
		status TEXT,
		message TEXT,
		details TEXT
	);`

	_, err = db.Exec(createAuditLog)
	if err != nil {
		return err
	}

	// Create index for faster queries
	db.Exec("CREATE INDEX IF NOT EXISTS idx_host_ip ON file_reports(host_ip);")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_timestamp ON file_reports(timestamp);")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_audit_host ON audit_logs(host_ip);")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_audit_date ON audit_logs(created_at);")

	log.Println("Database initialized successfully")
	return nil
}

// Write audit log to database
func writeAuditLog(hostIP, action, status, message, details string) {
	insertSQL := `INSERT INTO audit_logs (host_ip, action, status, message, details) 
		VALUES (?, ?, ?, ?, ?)`

	_, err := db.Exec(insertSQL, hostIP, action, status, message, details)
	if err != nil {
		log.Printf("Error writing audit log: %v", err)
	}

	// Also write to text file
	logFile := "audit_logs.txt"
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logLine := fmt.Sprintf("[%s] %s | %s | %s | %s\n", timestamp, hostIP, action, status, message)

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		f.WriteString(logLine)
	}
}

// Save file report to database with validation and deduplication
func saveFileReport(report FileReport) (int64, error) {
	// Check if host exists and has recent data with same size
	var existingID int64
	var existingReportData string

	err := db.QueryRow(`
		SELECT id, report_data FROM file_reports 
		WHERE host_ip = ? 
		ORDER BY timestamp DESC 
		LIMIT 1
	`, report.HostIP).Scan(&existingID, &existingReportData)

	// If host exists, check if data is identical
	if err == nil {
		var existingReport FileReport
		err := json.Unmarshal([]byte(existingReportData), &existingReport)
		if err == nil {
			// Calculate total size from directories
			newTotalSize := int64(0)
			for _, d := range report.Directories {
				newTotalSize += d.SizeBytes
			}

			existingTotalSize := int64(0)
			for _, d := range existingReport.Directories {
				existingTotalSize += d.SizeBytes
			}

			// If sizes are same, skip the report
			if newTotalSize == existingTotalSize {
				writeAuditLog(report.HostIP, "RECEIVE", "SKIPPED",
					"Data identical to last report, skipped", "")
				return existingID, nil
			}

			// Sizes are different, update the data
			writeAuditLog(report.HostIP, "UPDATE", "SUCCESS",
				fmt.Sprintf("Data changed: %d MB -> %d MB", existingTotalSize/1048576, newTotalSize/1048576), "")
		}
	} else if err != sql.ErrNoRows {
		// Log other database errors
		writeAuditLog(report.HostIP, "VALIDATE", "ERROR", "Database error checking existing data", err.Error())
		return 0, err
	} else {
		// New host
		writeAuditLog(report.HostIP, "RECEIVE", "NEW_HOST", "New host registered", "")
	}

	// Convert report to JSON for storage
	reportJSON, err := json.Marshal(report)
	if err != nil {
		writeAuditLog(report.HostIP, "SAVE", "ERROR", "Failed to marshal JSON", err.Error())
		return 0, err
	}

	// Calculate total files and size
	totalFiles := 0
	totalSize := int64(0)
	for _, dir := range report.Directories {
		totalFiles += dir.FileCount
		totalSize += dir.SizeBytes
	}

	// Insert main report
	insertSQL := `INSERT INTO file_reports 
		(host_ip, base_path, total_directories, report_data)
		VALUES (?, ?, ?, ?)`

	result, err := db.Exec(insertSQL,
		report.HostIP,
		report.BasePath,
		report.TotalDirectories,
		string(reportJSON))

	if err != nil {
		writeAuditLog(report.HostIP, "SAVE", "ERROR", "Failed to insert report", err.Error())
		return 0, err
	}

	reportID, err := result.LastInsertId()
	if err != nil {
		writeAuditLog(report.HostIP, "SAVE", "ERROR", "Failed to get report ID", err.Error())
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
		writeAuditLog(report.HostIP, "RECEIVE", "ERROR", "Failed to save report", err.Error())
		http.Error(w, "Failed to save report", http.StatusInternalServerError)
		return
	}

	// Calculate totals for logging
	totalFiles := 0
	totalSize := int64(0)
	for _, dir := range report.Directories {
		totalFiles += dir.FileCount
		totalSize += dir.SizeBytes
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	log.Printf("[%s] Report received from %s - ID: %d - Directories: %d, Files: %d, Size: %d MB",
		timestamp, report.HostIP, reportID, report.TotalDirectories, totalFiles, totalSize/1048576)

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
	// Get the latest report for each host
	query := `
		SELECT 
			host_ip,
			timestamp,
			report_data,
			total_directories
		FROM file_reports
		WHERE (host_ip, timestamp) IN (
			SELECT host_ip, MAX(timestamp) FROM file_reports GROUP BY host_ip
		)
		ORDER BY timestamp DESC
	`

	rows, err := db.Query(query)
	if err != nil {
		log.Printf("Database query error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(make([]interface{}, 0))
		return
	}
	defer rows.Close()

	hosts := make([]map[string]interface{}, 0)
	hostCountMap := make(map[string]int)

	for rows.Next() {
		var hostIP, timestamp, reportDataStr string
		var totalDirs int
		err := rows.Scan(&hostIP, &timestamp, &reportDataStr, &totalDirs)
		if err != nil {
			log.Printf("Row scan error: %v", err)
			continue
		}

		// Parse JSON report
		var report FileReport
		err = json.Unmarshal([]byte(reportDataStr), &report)
		if err != nil {
			log.Printf("JSON parse error: %v", err)
			continue
		}

		// Calculate totals from directories
		totalFiles := 0
		totalSizeBytes := int64(0)
		totalSizeMB := 0
		totalSizeGB := 0

		for _, dir := range report.Directories {
			totalFiles += dir.FileCount
			totalSizeBytes += dir.SizeBytes
		}

		if totalSizeBytes > 0 {
			totalSizeMB = int(totalSizeBytes / 1048576)
			totalSizeGB = int(totalSizeBytes / 1073741824)
		}

		// Count reports per host
		hostCountMap[hostIP]++

		hosts = append(hosts, map[string]interface{}{
			"host_ip":       hostIP,
			"last_report":   timestamp,
			"total_files":   totalFiles,
			"total_size_mb": totalSizeMB,
			"total_size_gb": totalSizeGB,
			"report_count":  hostCountMap[hostIP],
		})
	}

	// Get accurate report counts
	countQuery := `SELECT host_ip, COUNT(*) as count FROM file_reports GROUP BY host_ip`
	countRows, err := db.Query(countQuery)
	if err == nil {
		defer countRows.Close()
		for countRows.Next() {
			var hostIP string
			var count int
			if err := countRows.Scan(&hostIP, &count); err == nil {
				for i, host := range hosts {
					if host["host_ip"] == hostIP {
						hosts[i]["report_count"] = count
						break
					}
				}
			}
		}
	}

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
		SELECT id, host_ip, timestamp, report_data, total_directories
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
		var hostIPVal, reportDataStr string
		var timestamp time.Time
		var totalDirs int

		err := rows.Scan(&id, &hostIPVal, &timestamp, &reportDataStr, &totalDirs)
		if err != nil {
			continue
		}

		// Parse report_data JSON
		var report FileReport
		var totalFiles int
		var totalSizeBytes int64
		totalSizeMB := 0
		totalSizeGB := 0.0

		if err := json.Unmarshal([]byte(reportDataStr), &report); err == nil {
			for _, dir := range report.Directories {
				totalFiles += dir.FileCount
				totalSizeBytes += dir.SizeBytes
			}
			totalSizeMB = int(totalSizeBytes / (1024 * 1024))
			totalSizeGB = float64(totalSizeBytes) / (1024 * 1024 * 1024)
		}

		reports = append(reports, map[string]interface{}{
			"id":                id,
			"host_ip":           hostIPVal,
			"timestamp":         timestamp,
			"total_directories": totalDirs,
			"total_files":       totalFiles,
			"total_size_mb":     totalSizeMB,
			"total_size_gb":     totalSizeGB,
			"directories":       report.Directories,
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

// Get audit logs API
func getLogsHandler(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Query().Get("action")
	hostIP := r.URL.Query().Get("host_ip")
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "100"
	}

	query := `SELECT id, created_at, host_ip, action, status, message, details FROM audit_logs WHERE 1=1`
	var args []interface{}

	if action != "" {
		query += " AND action = ?"
		args = append(args, action)
	}
	if hostIP != "" {
		query += " AND host_ip = ?"
		args = append(args, hostIP)
	}

	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	var logs []map[string]interface{}
	for rows.Next() {
		var id int
		var createdAt, hostIP, action, status, message, details string
		err := rows.Scan(&id, &createdAt, &hostIP, &action, &status, &message, &details)
		if err != nil {
			continue
		}
		logs = append(logs, map[string]interface{}{
			"id":         id,
			"created_at": createdAt,
			"host_ip":    hostIP,
			"action":     action,
			"status":     status,
			"message":    message,
			"details":    details,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

// Logs page handler - serves the logs.html file
func logsPageHandler(w http.ResponseWriter, r *http.Request) {
	htmlContent, err := os.ReadFile("logs.html")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Logs file not found. Place logs.html in the same directory as the executable.",
			"error":   err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(htmlContent)
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
	mux.HandleFunc("/api/logs", getLogsHandler)
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/debug", debugHandler)

	// Pages
	mux.HandleFunc("/logs", logsPageHandler)
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
	fmt.Printf("  Logs:       http://localhost:%s/logs\n", config.Port)
	fmt.Printf("  API:        http://localhost:%s/command\n", config.Port)
	fmt.Printf("  Health:     http://localhost:%s/health\n\n", config.Port)

	log.Fatal(http.ListenAndServe(":"+config.Port, handler))
}

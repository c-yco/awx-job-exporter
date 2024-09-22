package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/viper"
)

var (
	awxJobsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "awx_jobs_total",
			Help: "Total number of AWX jobs per organization, status, and job labels.",
		},
		[]string{"organization", "status", "job_labels"},
	)

	whitelistOrganizations []string
	whitelistLabels        []string
	whitelistEnabled       bool
)

func init() {
	prometheus.MustRegister(awxJobsTotal)
	loadConfig()
}

type AWXJob struct {
	Status string `json:"status"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"summary_fields.labels"`
	Organization struct {
		Name string `json:"name"`
	} `json:"summary_fields.organization"`
	JobId   int     `json:"id"`
	Elapsed float32 `json:"elapsed"`
}

type AWXResponse struct {
	Results []AWXJob `json:"results"`
}

func loadConfig() {
	viper.SetConfigName("config") // Name der Konfigurationsdatei ohne Erweiterung
	viper.SetConfigType("yaml")   // Dateityp
	viper.AddConfigPath(".")      // Suchpfad
	viper.AutomaticEnv()          // Lädt Umgebungsvariablen automatisch

	// Standardwerte setzen
	viper.SetDefault("awx.api_url", "http://your-awx-url/api/v2/jobs/")
	viper.SetDefault("awx.username", "your-username")
	viper.SetDefault("awx.password", "your-password")

	err := viper.ReadInConfig() // Versuche, die Konfigurationsdatei zu lesen
	if err != nil {
		log.Fatalf("Error loading config file: %v", err)
	}

	whitelistOrganizations = viper.GetStringSlice("whitelist.organizations")
	whitelistLabels = viper.GetStringSlice("whitelist.labels")
	whitelistEnabled = viper.GetBool("whitelist.enabled")

	log.Println("Loaded configuration:")
	log.Printf("AWX API URL: %s", viper.GetString("awx.api_url"))
	log.Printf("Whitelist enabled: %v", whitelistEnabled)
	log.Printf("Whitelisted organizations: %v", whitelistOrganizations)
	log.Printf("Whitelisted labels: %v", whitelistLabels)
}

func fetchAWXJobData(apiURL, username, password string) (*AWXResponse, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(username, password)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result AWXResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

func isWhitelisted(organization string, jobLabels []string) bool {
	// Überprüfe, ob die Organisation in der Whitelist ist
	if !contains(whitelistOrganizations, organization) {
		return false
	}

	// Überprüfe, ob mindestens ein Label in der Whitelist ist
	for _, label := range jobLabels {
		if contains(whitelistLabels, label) {
			return true
		}
	}

	return false
}

func contains(slice []string, item string) bool {
	for _, element := range slice {
		if element == item {
			return true
		}
	}
	return false
}

func recordAWXMetrics() {
	go func() {
		for {
			apiURL := viper.GetString("awx.api_url")
			username := viper.GetString("awx.username")
			password := viper.GetString("awx.password")

			awxResponse, err := fetchAWXJobData(apiURL, username, password)
			if err != nil {
				log.Printf("Error fetching AWX job data: %v", err)
				continue
			}

			jobCountByOrgStatusAndLabel := make(map[string]map[string]map[string]int)

			for _, job := range awxResponse.Results {
				orgName := job.Organization.Name
				jobID := job.JobId
				// Kombiniere alle Job-Labels zu einem String
				var jobLabels []string
				for _, label := range job.Labels {
					jobLabels = append(jobLabels, label.Name)
				}
				combinedLabels := strings.Join(jobLabels, ",")

				// Filtere Jobs nach Whitelist
				if whitelistEnabled {
					if !isWhitelisted(orgName, jobLabels) {
						log.Printf("Ignoring JobID as its not on the whitelist %v", jobID)
						continue
					}
				}

				jobStatus := job.Status

				if _, exists := jobCountByOrgStatusAndLabel[orgName]; !exists {
					jobCountByOrgStatusAndLabel[orgName] = make(map[string]map[string]int)
				}
				if _, exists := jobCountByOrgStatusAndLabel[orgName][jobStatus]; !exists {
					jobCountByOrgStatusAndLabel[orgName][jobStatus] = make(map[string]int)
				}

				jobCountByOrgStatusAndLabel[orgName][jobStatus][combinedLabels]++
			}

			// Aktualisiere die Metriken mit den gesammelten Daten
			for org, statusMap := range jobCountByOrgStatusAndLabel {
				for status, labelMap := range statusMap {
					for labels, count := range labelMap {
						awxJobsTotal.WithLabelValues(org, status, labels).Set(float64(count))
					}
				}
			}

			time.Sleep(10 * time.Second)
		}
	}()
}

func main() {
	recordAWXMetrics()

	http.Handle("/metrics", promhttp.Handler())
	log.Println("AWX Job Exporter started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	vegeta "github.com/tsenart/vegeta/v12/lib"
)

var baseURL = flag.String("api-url", "http://localhost:8000", "URL for the test API")

var (
	teamNames = []string{"backend", "frontend", "qateam"}
	userIDs   = []string{
		"00000000-0000-0000-0000-000000000001", // Alice (Backend)
		"00000000-0000-0000-0000-000000000002", // Bob (Backend)
		"00000000-0000-0000-0000-000000000003", // Charlie (Backend)
		"00000000-0000-0000-0000-000000000004", // David (Frontend)
		"00000000-0000-0000-0000-000000000005", // Eve (Frontend)
		"00000000-0000-0000-0000-000000000006", // Frank (Frontend)
		"00000000-0000-0000-0000-000000000007", // Grace (QA)
		"00000000-0000-0000-0000-000000000008", // Henry (QA)
	}
)

type Config struct {
	Duration time.Duration
	Rate     uint64
	Command  string
}

func main() {
	cmd := flag.String("cmd", "test", "Command: setup, test")
	duration := flag.Duration("duration", 10*time.Second, "Test duration")
	rate := flag.Uint64("rate", 50, "Requests per second")
	flag.Parse()

	config := Config{
		Duration: *duration,
		Rate:     *rate,
		Command:  *cmd,
	}

	switch config.Command {
	case "setup":
		setupTestData()
	case "test":
		runLoadTest(config)
	default:
		fmt.Println("Unknown command. Use: setup, test")
		os.Exit(1)
	}
}

func setupTestData() {
	fmt.Println("Setting up consistent test data...")

	teams := []struct {
		Name    string
		Members []map[string]interface{}
	}{
		{
			Name: "backend",
			Members: []map[string]interface{}{
				{"user_id": userIDs[0], "username": "Alice", "is_active": true},
				{"user_id": userIDs[1], "username": "Bob", "is_active": true},
				{"user_id": userIDs[2], "username": "Charlie", "is_active": true},
			},
		},
		{
			Name: "frontend",
			Members: []map[string]interface{}{
				{"user_id": userIDs[3], "username": "David", "is_active": true},
				{"user_id": userIDs[4], "username": "Eve", "is_active": true},
				{"user_id": userIDs[5], "username": "Frank", "is_active": true},
			},
		},
		{
			Name: "qateam",
			Members: []map[string]interface{}{
				{"user_id": userIDs[6], "username": "Grace", "is_active": true},
				{"user_id": userIDs[7], "username": "Henry", "is_active": true},
			},
		},
	}

	for _, team := range teams {
		payload := map[string]interface{}{
			"team_name": team.Name,
			"members":   team.Members,
		}
		makeRequest("POST", "/team/add", payload)
		time.Sleep(50 * time.Millisecond)
	}

	fmt.Println("Test data setup completed")
}

func runLoadTest(config Config) {
	fmt.Printf("Running SLI validation test: duration=%v, rate=%d req/s\n", config.Duration, config.Rate)

	targeter := generateStrictTargeter()
	attacker := vegeta.NewAttacker()

	var metrics vegeta.Metrics
	for res := range attacker.Attack(targeter, vegeta.Rate{Freq: int(config.Rate), Per: time.Second}, config.Duration, "SLI Test") {
		metrics.Add(res)
	}
	metrics.Close()

	printReport(&metrics)
	validateSLI(&metrics)
}

func generateStrictTargeter() vegeta.Targeter {
	var counter uint64

	return func(tgt *vegeta.Target) error {
		roll := rand.IntN(10)
		idx := atomic.AddUint64(&counter, 1)

		switch {
		// --- READ OPERATIONS (Low Risk, High Volume) ---
		case roll < 3: // 30% Health Check
			tgt.Method = "GET"
			tgt.URL = *baseURL + "/health"

		case roll < 5: // 20% Get Stats
			tgt.Method = "GET"
			tgt.URL = *baseURL + "/stats/assignments"

		case roll < 7: // 20% Get Existing Team
			team := teamNames[rand.IntN(len(teamNames))]
			tgt.Method = "GET"
			tgt.URL = *baseURL + "/team/get?team_name=" + team

		case roll < 8: // 10% Get User Review (Existing Users)
			user := userIDs[rand.IntN(len(userIDs))]
			tgt.Method = "GET"
			tgt.URL = *baseURL + "/users/getReview?user_id=" + user

		// --- WRITE OPERATIONS (Must be unique to avoid 409) ---
		case roll == 8: // 10% Create New Team
			// Ensure unique team name every time
			uniqueTeamName := fmt.Sprintf("load_team_%d_%s", idx, uuid.New().String()[:8])

			members := []map[string]interface{}{
				{
					"user_id":   uuid.New().String(),
					"username":  fmt.Sprintf("load_user_%d", idx),
					"is_active": true,
				},
			}
			body, _ := json.Marshal(map[string]interface{}{
				"team_name": uniqueTeamName,
				"members":   members,
			})
			tgt.Method = "POST"
			tgt.URL = *baseURL + "/team/add"
			tgt.Body = body
			tgt.Header = http.Header{"Content-Type": []string{"application/json"}}

		default: // 10% Create PR
			// Ensure unique PR ID and name
			author := userIDs[rand.IntN(len(userIDs))]
			body, _ := json.Marshal(map[string]interface{}{
				"pull_request_id":   uuid.New().String(),
				"pull_request_name": fmt.Sprintf("Load_Feature_%d", idx),
				"author_id":         author,
			})
			tgt.Method = "POST"
			tgt.URL = *baseURL + "/pullRequest/create"
			tgt.Body = body
			tgt.Header = http.Header{"Content-Type": []string{"application/json"}}
		}

		return nil
	}
}

func makeRequest(method, path string, body interface{}) {
	var bodyReader io.Reader
	if body != nil {
		jsonData, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequest(method, *baseURL+path, bodyReader)
	if err != nil {
		return
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err == nil {
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				log.Printf("Warning: failed to close response body: %v", closeErr)
			}
		}()
		fmt.Printf("%s %s -> %d\n", method, path, resp.StatusCode)
	}
}

func printReport(metrics *vegeta.Metrics) {
	fmt.Println("\n=== Test Results ===")
	fmt.Printf("Requests:      %d\n", metrics.Requests)
	fmt.Printf("Success Rate:  %.4f%%\n", metrics.Success*100)
	fmt.Printf("Mean Latency:  %v\n", metrics.Latencies.Mean)
	fmt.Printf("P95 Latency:   %v\n", metrics.Latencies.P95)
	fmt.Printf("P99 Latency:   %v\n", metrics.Latencies.P99)

	fmt.Println("\n--- Status Codes ---")
	for code, count := range metrics.StatusCodes {
		fmt.Printf("%s: %d\n", code, count)
	}
}

func validateSLI(metrics *vegeta.Metrics) {
	fmt.Println("\n=== SLI Validation ===")

	// Strict targets
	successTarget := 0.999 // 99.9%
	latencyTarget := 300 * time.Millisecond

	successPass := metrics.Success >= successTarget
	latencyPass := metrics.Latencies.P95 <= latencyTarget

	fmt.Printf("Success Rate: %.4f%% (target: >= %.3f%%) [%s]\n",
		metrics.Success*100, successTarget*100, status(successPass))

	fmt.Printf("P95 Latency:  %v (target: <= %v) [%s]\n",
		metrics.Latencies.P95, latencyTarget, status(latencyPass))

	if successPass && latencyPass {
		fmt.Println("\n✓ All SLI targets met successfully")
		os.Exit(0)
	}
	fmt.Println("\n✗ SLI targets violated")
	os.Exit(1)
}

func status(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}

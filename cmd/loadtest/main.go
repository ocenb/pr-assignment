package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ocenb/pr-assignment/internal/api"
	vegeta "github.com/tsenart/vegeta/v12/lib"
)

var baseURL = flag.String("api-url", "http://localhost:8000", "URL for the test API")

var (
	teamNames = []api.TeamName{"backend", "frontend", "qateam"}
	userIDs   = []uuid.UUID{
		uuid.MustParse("00000000-0000-0000-0000-000000000001"), // Alice (Backend)
		uuid.MustParse("00000000-0000-0000-0000-000000000002"), // Bob (Backend)
		uuid.MustParse("00000000-0000-0000-0000-000000000003"), // Charlie (Backend)
		uuid.MustParse("00000000-0000-0000-0000-000000000004"), // David (Frontend)
		uuid.MustParse("00000000-0000-0000-0000-000000000005"), // Eve (Frontend)
		uuid.MustParse("00000000-0000-0000-0000-000000000006"), // Frank (Frontend)
		uuid.MustParse("00000000-0000-0000-0000-000000000007"), // Grace (QA)
		uuid.MustParse("00000000-0000-0000-0000-000000000008"), // Henry (QA)
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

	client, err := api.NewClient(*baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create client: %v\n", err)
		os.Exit(1)
	}

	switch config.Command {
	case "setup":
		setupTestData(client)
	case "test":
		if !runLoadTest(config, client) {
			os.Exit(1)
		}
	default:
		fmt.Println("Unknown command. Use: setup, test")
		os.Exit(1)
	}
}

func setupTestData(client *api.Client) {
	fmt.Println("Setting up consistent test data...")
	ctx := context.Background()

	teams := []api.Team{
		{
			TeamName: "backend",
			Members: []api.TeamMember{
				{UserID: userIDs[0], Username: "Alice", IsActive: true},
				{UserID: userIDs[1], Username: "Bob", IsActive: true},
				{UserID: userIDs[2], Username: "Charlie", IsActive: true},
			},
		},
		{
			TeamName: "frontend",
			Members: []api.TeamMember{
				{UserID: userIDs[3], Username: "David", IsActive: true},
				{UserID: userIDs[4], Username: "Eve", IsActive: true},
				{UserID: userIDs[5], Username: "Frank", IsActive: true},
			},
		},
		{
			TeamName: "qateam",
			Members: []api.TeamMember{
				{UserID: userIDs[6], Username: "Grace", IsActive: true},
				{UserID: userIDs[7], Username: "Henry", IsActive: true},
			},
		},
	}

	for _, team := range teams {
		_, err := client.CreateTeam(ctx, &team)
		if err != nil {
			fmt.Printf("Failed to create team %s: %v\n", team.TeamName, err)
		} else {
			fmt.Printf("POST /team/add -> 201 (%s)\n", team.TeamName)
		}
		time.Sleep(50 * time.Millisecond)
	}

	fmt.Println("Test data setup completed")
}

func runLoadTest(config Config, client *api.Client) bool {
	fmt.Printf("Running SLI validation test: duration=%v, rate=%d req/s\n", config.Duration, config.Rate)

	var metrics vegeta.Metrics
	var wg sync.WaitGroup
	var counter uint64

	resCh := make(chan *vegeta.Result, config.Rate*2)
	doneCh := make(chan struct{})

	go func() {
		for res := range resCh {
			metrics.Add(res)
		}
		close(doneCh)
	}()

	ticker := time.NewTicker(time.Second / time.Duration(config.Rate))
	defer ticker.Stop()

	timeout := time.After(config.Duration)
	ctx := context.Background()

loop:
	for {
		select {
		case <-timeout:
			break loop
		case <-ticker.C:
			wg.Add(1)
			counter++
			idx := counter
			go func(i uint64) {
				defer wg.Done()
				start := time.Now()
				code, err := executeOperation(ctx, client, i)
				latency := time.Since(start)

				res := &vegeta.Result{
					Code:      uint16(code),
					Timestamp: start,
					Latency:   latency,
				}

				if err != nil {
					res.Error = err.Error()
				}

				resCh <- res
			}(idx)
		}
	}

	wg.Wait()
	close(resCh)
	<-doneCh

	metrics.Close()
	printReport(&metrics)
	return validateSLI(&metrics)
}

func executeOperation(ctx context.Context, client *api.Client, idx uint64) (int, error) {
	roll := rand.IntN(10)

	switch {
	// --- READ OPERATIONS (Low Risk, High Volume) ---
	case roll < 3: // 30% Health Check
		res, err := client.CheckHealth(ctx)
		if err != nil {
			return 0, err
		}
		if _, ok := res.(*api.CheckHealthOK); ok {
			return 200, nil
		}
		return 500, fmt.Errorf("internal server error")

	case roll < 5: // 20% Get Stats
		res, err := client.GetAssignmentStats(ctx)
		if err != nil {
			return 0, err
		}
		if _, ok := res.(*api.GetAssignmentStatsOK); ok {
			return 200, nil
		}
		return 500, fmt.Errorf("internal server error")

	case roll < 7: // 20% Get Existing Team
		teamName := teamNames[rand.IntN(len(teamNames))]
		res, err := client.GetTeam(ctx, api.GetTeamParams{TeamName: teamName})
		if err != nil {
			return 0, err
		}
		switch res.(type) {
		case *api.Team:
			return 200, nil
		case *api.GetTeamBadRequest:
			return 400, fmt.Errorf("bad request")
		case *api.GetTeamNotFound:
			return 404, fmt.Errorf("team not found")
		default:
			return 500, fmt.Errorf("internal server error")
		}

	case roll < 8: // 10% Get User Review (Existing Users)
		user := userIDs[rand.IntN(len(userIDs))]
		res, err := client.GetUserReviews(ctx, api.GetUserReviewsParams{UserID: user})
		if err != nil {
			return 0, err
		}
		switch res.(type) {
		case *api.GetUserReviewsOK:
			return 200, nil
		case *api.GetUserReviewsBadRequest:
			return 400, fmt.Errorf("bad request")
		case *api.GetUserReviewsNotFound:
			return 404, fmt.Errorf("user not found")
		default:
			return 500, fmt.Errorf("internal server error")
		}

	// --- WRITE OPERATIONS (Must be unique to avoid 409) ---
	case roll == 8: // 10% Create New Team
		uniqueTeamName := fmt.Sprintf("load_team_%d_%s", idx, uuid.New().String()[:8])
		req := &api.Team{
			TeamName: api.TeamName(uniqueTeamName),
			Members: []api.TeamMember{
				{
					UserID:   uuid.New(),
					Username: api.Username(fmt.Sprintf("load_user_%d", idx)),
					IsActive: true,
				},
			},
		}
		res, err := client.CreateTeam(ctx, req)
		if err != nil {
			return 0, err
		}
		switch res.(type) {
		case *api.CreateTeamCreated:
			return 201, nil
		case *api.CreateTeamBadRequest:
			return 400, fmt.Errorf("bad request")
		case *api.CreateTeamConflict:
			return 409, fmt.Errorf("conflict")
		default:
			return 500, fmt.Errorf("internal server error")
		}

	default: // 10% Create PR
		author := userIDs[rand.IntN(len(userIDs))]
		req := &api.CreatePullRequestReq{
			PullRequestID:   uuid.New(),
			PullRequestName: api.PullRequestName(fmt.Sprintf("Load_Feature_%d", idx)),
			AuthorID:        author,
		}
		res, err := client.CreatePullRequest(ctx, req)
		if err != nil {
			return 0, err
		}
		switch res.(type) {
		case *api.CreatePullRequestCreated:
			return 201, nil
		case *api.CreatePullRequestBadRequest:
			return 400, fmt.Errorf("bad request")
		case *api.CreatePullRequestNotFound:
			return 404, fmt.Errorf("not found")
		case *api.CreatePullRequestConflict:
			return 409, fmt.Errorf("conflict")
		default:
			return 500, fmt.Errorf("internal server error")
		}
	}
}

func printReport(metrics *vegeta.Metrics) {
	fmt.Println("\n=== Test Results ===")
	fmt.Printf("Requests: %d\n", metrics.Requests)
	fmt.Printf("Success Rate: %.4f%%\n", metrics.Success*100)
	fmt.Printf("Mean Latency: %v\n", metrics.Latencies.Mean)
	fmt.Printf("P95 Latency: %v\n", metrics.Latencies.P95)
	fmt.Printf("P99 Latency: %v\n", metrics.Latencies.P99)

	fmt.Println("\n--- Status Codes ---")
	for code, count := range metrics.StatusCodes {
		fmt.Printf("%s: %d\n", code, count)
	}
}

func validateSLI(metrics *vegeta.Metrics) bool {
	fmt.Println("\n=== SLI Validation ===")

	successTarget := 0.999 // 99.9%
	latencyTarget := 300 * time.Millisecond

	successPass := metrics.Success >= successTarget
	latencyPass := metrics.Latencies.P95 <= latencyTarget

	fmt.Printf("Success Rate: %.4f%% (target: >= %.3f%%) [%s]\n",
		metrics.Success*100, successTarget*100, status(successPass))
	fmt.Printf("P95 Latency: %v (target: <= %v) [%s]\n",
		metrics.Latencies.P95, latencyTarget, status(latencyPass))

	if successPass && latencyPass {
		fmt.Println("\n✓ All SLI targets met successfully")
		return true
	}
	fmt.Println("\n✗ SLI targets violated")
	return false
}

func status(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}

package tests

import (
	"context"
	"flag"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ocenb/pr-assignment/internal/api"
	"github.com/stretchr/testify/require"
)

var apiURL = flag.String("api-url", "http://localhost:8000", "URL for the test API")

func TestE2E_FullFlow(t *testing.T) {
	t.Parallel()

	// 1. Setup Client
	client, err := api.NewClient(*apiURL, api.WithClient(http.DefaultClient))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 2. Health check
	health, err := client.CheckHealth(ctx)
	require.NoError(t, err)
	require.IsType(t, &api.CheckHealthOK{}, health)

	// 3. Data Preparation
	teamName := api.TeamName("team-" + uuid.New().String())
	user1ID := uuid.New() // Author
	user2ID := uuid.New() // Reviewer 1
	user3ID := uuid.New() // Reviewer 2
	user4ID := uuid.New() // Replacement Candidate (initially inactive)

	// Create Team with 4 members. User4 is inactive.
	createTeamReq := &api.Team{
		TeamName: teamName,
		Members: []api.TeamMember{
			{UserID: user1ID, Username: "user1", IsActive: true},
			{UserID: user2ID, Username: "user2", IsActive: true},
			{UserID: user3ID, Username: "user3", IsActive: true},
			{UserID: user4ID, Username: "user4", IsActive: false},
		},
	}

	// 4. Create Team
	createTeamResp, err := client.CreateTeam(ctx, createTeamReq)
	require.NoError(t, err)
	createdTeam, ok := createTeamResp.(*api.CreateTeamCreated)
	require.True(t, ok)
	require.True(t, createdTeam.Team.IsSet())
	require.Equal(t, teamName, createdTeam.Team.Value.TeamName)
	require.Len(t, createdTeam.Team.Value.Members, 4)

	// 5. Create Pull Request
	prID := uuid.New()
	createPRReq := &api.CreatePullRequestReq{
		PullRequestID:   prID,
		PullRequestName: "My first PR",
		AuthorID:        user1ID,
	}
	createPRResp, err := client.CreatePullRequest(ctx, createPRReq)
	require.NoError(t, err)
	createdPR, ok := createPRResp.(*api.CreatePullRequestCreated)
	require.True(t, ok)

	pr := createdPR.Pr.Value
	require.Equal(t, prID, pr.PullRequestID)
	require.Equal(t, user1ID, pr.AuthorID)
	require.Equal(t, api.PullRequestStatusOPEN, pr.Status)

	// Since User4 is inactive, reviewers must be User2 and User3
	require.Len(t, pr.AssignedReviewers, 2)
	require.Contains(t, pr.AssignedReviewers, user2ID)
	require.Contains(t, pr.AssignedReviewers, user3ID)
	require.NotContains(t, pr.AssignedReviewers, user4ID)

	// 6. Verify initial assignment
	reviewerToReassign := user3ID

	userReviewsResp, err := client.GetUserReviews(ctx, api.GetUserReviewsParams{UserID: reviewerToReassign})
	require.NoError(t, err)
	userReviews, ok := userReviewsResp.(*api.GetUserReviewsOK)
	require.True(t, ok)
	require.Equal(t, reviewerToReassign, userReviews.UserID)
	require.Len(t, userReviews.PullRequests, 1)

	// 7. Manual Reassignment Conflict Test (No Candidates)
	// Scenario: Try to reassign User3.
	// Candidates: User1 (Author - invalid), User2 (Already reviewer - invalid), User4 (Inactive - invalid).
	// Expected: NO_CANDIDATE error.
	reassignReq := &api.ReassignReviewerReq{
		PullRequestID: prID,
		OldUserID:     reviewerToReassign,
	}

	reassignResp, err := client.ReassignReviewer(ctx, reassignReq)
	require.NoError(t, err)

	reassignConflict, ok := reassignResp.(*api.ReassignReviewerConflict)
	require.Truef(t, ok, "expected Conflict response, got %T", reassignResp)
	require.Equal(t, api.ErrorResponseErrorCodeNOCANDIDATE, reassignConflict.Error.Code)

	// 8. Manual Reassignment Success Test
	// Activate User4. He becomes a valid candidate.
	_, err = client.SetUserActive(ctx, &api.SetUserActiveReq{
		UserID:   user4ID,
		IsActive: true,
	})
	require.NoError(t, err)

	// Retry Reassign
	reassignResp, err = client.ReassignReviewer(ctx, reassignReq)
	require.NoError(t, err)
	reassignedPR, ok := reassignResp.(*api.ReassignReviewerOK)
	require.True(t, ok, "reassignment should be successful now that User4 is active")

	require.Equal(t, user4ID, reassignedPR.ReplacedBy)
	require.Len(t, reassignedPR.Pr.AssignedReviewers, 2)
	require.NotContains(t, reassignedPR.Pr.AssignedReviewers, reviewerToReassign) // User3 gone
	require.Contains(t, reassignedPR.Pr.AssignedReviewers, user4ID)               // User4 added

	// 9. Auto-Reassignment Test
	// Current Reviewers: User2, User4.
	// User3 is active but not assigned.
	// Scenario: Deactivate User2. System should automatically replace User2 -> User3.

	_, err = client.SetUserActive(ctx, &api.SetUserActiveReq{
		UserID:   user2ID,
		IsActive: false,
	})
	require.NoError(t, err)

	// Verify User3 got the review assignment automatically
	require.Eventually(t, func() bool {
		resp, err := client.GetUserReviews(ctx, api.GetUserReviewsParams{UserID: user3ID})
		if err != nil {
			return false
		}
		reviews, ok := resp.(*api.GetUserReviewsOK)
		if !ok {
			return false
		}
		for _, p := range reviews.PullRequests {
			if p.PullRequestID == prID {
				return true
			}
		}
		return false
	}, 2*time.Second, 100*time.Millisecond, "User3 should be auto-assigned after User2 deactivation")

	// 10. Merge Pull Request
	mergePRReq := &api.MergePullRequestReq{PullRequestID: prID}
	mergePRResp, err := client.MergePullRequest(ctx, mergePRReq)
	require.NoError(t, err)
	mergedPR, ok := mergePRResp.(*api.MergePullRequestOK)
	require.True(t, ok)
	require.Equal(t, api.PullRequestStatusMERGED, mergedPR.Pr.Value.Status)

	// 11. Verify Immutability of Merged PR
	// Try to reassign on a merged PR (User4 -> User2)
	reassignMergedReq := &api.ReassignReviewerReq{
		PullRequestID: prID,
		OldUserID:     user4ID,
	}
	reassignMergedResp, err := client.ReassignReviewer(ctx, reassignMergedReq)
	require.NoError(t, err)

	conflictMerged, ok := reassignMergedResp.(*api.ReassignReviewerConflict)
	require.True(t, ok, "Should not allow reassignment on MERGED PR")
	require.Equal(t, api.ErrorResponseErrorCodePRMERGED, conflictMerged.Error.Code)

	// 12. Get Stats
	statsResp, err := client.GetAssignmentStats(ctx)
	require.NoError(t, err)
	stats, ok := statsResp.(*api.GetAssignmentStatsOK)
	require.True(t, ok)
	require.NotEmpty(t, stats.UserStats)
	require.NotEmpty(t, stats.PrStats)

	// 13. Cleanup: Deactivate Team
	deactivateReq := &api.DeactivateTeamMembersReq{TeamName: teamName}
	deactivateResp, err := client.DeactivateTeamMembers(ctx, deactivateReq)
	require.NoError(t, err)
	deactivated, ok := deactivateResp.(*api.DeactivateTeamMembersOK)
	require.True(t, ok)
	require.True(t, deactivated.DeactivatedCount > 0)

	// Verify deactivation
	getTeamResp, err := client.GetTeam(ctx, api.GetTeamParams{TeamName: teamName})
	require.NoError(t, err)
	fetchedTeam, ok := getTeamResp.(*api.Team)
	require.True(t, ok)
	for _, member := range fetchedTeam.Members {
		require.False(t, member.IsActive)
	}
}

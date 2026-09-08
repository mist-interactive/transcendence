package db

import (
	"context"
	"dbBackend/models"
	"log"

	"github.com/uptrace/bun"
	"golang.org/x/crypto/bcrypt"
)

func SeedDevDatabase(ctx context.Context, db *bun.DB) error {
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	names := []string{"nraatika", "mhirvasm", "jpelline", "anpollan", "zfarah", "loser1", "loser2", "loser3", "loser4", "loser5"}

	//build and bulk insert 10 users
	users := make([]models.User, len(names))
	for i, name := range names {
		users[i] = models.User{Username: name, Email: name + "@student.hive.fi", PWHash: string(hash)}
	}
	err := db.NewInsert().Model(&users).Scan(ctx)
	if err != nil {
		return err
	}

	//add 10 accepted friendships into DB
	friendships := make([]models.Friendship, len(users))
	for i := range users {
		friendships[i] = models.Friendship{
			UserID:   users[i].ID,
			FriendID: users[(i+1)%len(users)].ID, // connects user 0->1, 1->2, ... 9->0
			Status:   models.StatusAccepted,
		}
	}
	_, err = db.NewInsert().Model(&friendships).Exec(ctx)
	if err != nil {
		return err
	}

	// generate 50 matches with varied pairings and scores
	matches := make([]models.MatchRecord, 50)
	for i := range 50 {
		var win models.MatchResult
		var p1Score, p2Score int
		switch i % 3 {
		case 0:
			win = models.ResultPlayer1Win
			p1Score = 5 + (i % 3)
			p2Score = 2 + (i % 2)
		case 1:
			win = models.ResultPlayer2Win
			p1Score = 1 + (i % 3)
			p2Score = 6 + (i % 2)
		default:
			win = models.ResultDraw
			p1Score = 4
			p2Score = 4
		}
		matches[i] = models.MatchRecord{
			Player1:      users[i%len(users)].ID,
			Player2:      users[(i+3)%len(users)].ID,
			Player1Score: &p1Score,
			Player2Score: &p2Score,
			Status:       models.StatusFinished,
			Result:       &win,
		}
	}
	_, err = db.NewInsert().Model(&matches).Exec(ctx)
	if err != nil {
		return err
	}

	log.Printf("Successfully seeded %d users, %d friendships, and %d matches!", len(users), len(friendships), len(matches))
	return nil
}

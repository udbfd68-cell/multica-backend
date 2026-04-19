package main

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EnsureDefaultUser creates the default admin user, workspace, and membership
// if they don't exist. Called at startup when auth is disabled.
func EnsureDefaultUser(ctx context.Context, pool *pgxpool.Pool) {
	queries := db.New(pool)

	// Find or create user
	user, err := queries.GetUserByEmail(ctx, middleware.DefaultUserEmail)
	if err != nil {
		user, err = queries.CreateUser(ctx, db.CreateUserParams{
			Name:  "Admin",
			Email: middleware.DefaultUserEmail,
		})
		if err != nil {
			log.Printf("WARN: could not create default user: %v", err)
			return
		}
		log.Printf("Created default user: %s", middleware.DefaultUserEmail)
	}

	// Set the global default user ID
	middleware.DefaultUserID = util.UUIDToString(user.ID)
	log.Printf("Default user ID: %s", middleware.DefaultUserID)

	// Find or create default workspace
	ws, err := queries.GetWorkspaceBySlug(ctx, "main")
	if err != nil {
		ws, err = queries.CreateWorkspace(ctx, db.CreateWorkspaceParams{
			Name:        "Main",
			Slug:        "main",
			IssuePrefix: "AUR",
		})
		if err != nil {
			log.Printf("WARN: could not create default workspace: %v", err)
			return
		}
		log.Printf("Created default workspace: main")
	}

	// Ensure membership
	_, err = queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      user.ID,
		WorkspaceID: ws.ID,
	})
	if err != nil {
		_, err = queries.CreateMember(ctx, db.CreateMemberParams{
			WorkspaceID: ws.ID,
			UserID:      user.ID,
			Role:        "owner",
		})
		if err != nil {
			log.Printf("WARN: could not create default membership: %v", err)
			return
		}
		log.Printf("Created default membership for user in workspace main")
	}
}

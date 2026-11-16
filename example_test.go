// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "modernc.org/sqlite"

	migrate "github.com/go-gen-ecosystem/go-migrate"
)

// Apply the example migrations to an in-memory SQLite database. In an
// application, pass an embed.FS (via fs.Sub) instead of os.DirFS.
func Example() {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1) // one shared in-memory database

	m, err := migrate.New(db, "sqlite", migrate.WithFS(os.DirFS("examples/migrations")))
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	res, err := m.Up(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, a := range res.Applied {
		fmt.Println("applied", a.Source)
	}

	st, err := m.Status(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("current version:", st.Current, "pending:", st.Pending)

	plan, err := m.PlanDown(ctx, 1)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("down 1 would revert:", plan[0].Version)
	if err := db.Close(); err != nil {
		log.Fatal(err)
	}

	// Output:
	// applied 0001_create_users.sql
	// applied 0002_create_posts.up.sql | 0002_create_posts.down.sql
	// applied 0003_add_users_active.sql
	// applied R__user_post_counts.sql
	// current version: 3 pending: 0
	// down 1 would revert: 3
}

// Go migrations run in the same ordered sequence as SQL files.
func ExampleWithGoMigrations() {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1)

	backfill := migrate.GoMigration{
		Version:     "4",
		Description: "seed admin user",
		Up: func(ctx context.Context, ex migrate.Executor) error {
			_, err := ex.ExecContext(ctx, "INSERT INTO users (id, email, name) VALUES (1, 'admin@example.com', 'Admin')")
			return err
		},
		Down: func(ctx context.Context, ex migrate.Executor) error {
			_, err := ex.ExecContext(ctx, "DELETE FROM users WHERE id = 1")
			return err
		},
	}
	m, err := migrate.New(db, "sqlite",
		migrate.WithFS(os.DirFS("examples/migrations")),
		migrate.WithGoMigrations(backfill))
	if err != nil {
		log.Fatal(err)
	}
	if _, err := m.Up(context.Background()); err != nil {
		log.Fatal(err)
	}
	var posts int
	if err := db.QueryRow("SELECT posts FROM user_post_counts WHERE id = 1").Scan(&posts); err != nil {
		log.Fatal(err)
	}
	fmt.Println("admin has", posts, "posts")
	if err := db.Close(); err != nil {
		log.Fatal(err)
	}
	// Output: admin has 0 posts
}

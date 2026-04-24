package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/davitostes/maily/auth"
	"github.com/davitostes/maily/gmail"
	"github.com/davitostes/maily/ui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	if !auth.HasCredentials() {
		return fmt.Errorf(
			"Gmail credentials not found.\n\n"+
				"Expected file: %s\n\n"+
				"To create it:\n"+
				"  1. Visit https://console.cloud.google.com/\n"+
				"  2. Create a project and enable the Gmail API.\n"+
				"  3. Create an OAuth 2.0 Client ID (type: Desktop or Web).\n"+
				"  4. Download the client secret JSON and save it at the path above.",
			auth.CredentialsPath(),
		)
	}

	httpClient, _, _, err := auth.Client(ctx)
	if err != nil {
		return fmt.Errorf("authorize: %w", err)
	}

	client, err := gmail.New(ctx, httpClient)
	if err != nil {
		return fmt.Errorf("gmail client: %w", err)
	}

	model := ui.New(ctx, client)
	prog := tea.NewProgram(ui.Root(model), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := prog.Run(); err != nil {
		return err
	}
	return nil
}

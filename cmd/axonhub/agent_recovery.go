package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/looplj/axonhub/internal/ent"
	_ "github.com/looplj/axonhub/internal/ent/runtime"
	_ "github.com/looplj/axonhub/internal/pkg/sqlite"
	"github.com/looplj/axonhub/internal/server/orchestrator"
)

func handleAgentRecovery() {
	if err := runAgentRecovery(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "Agent message recovery:", err)
		os.Exit(1)
	}
}

func runAgentRecovery(args []string) error {
	flags := flag.NewFlagSet("agent-recovery", flag.ContinueOnError)
	database := flags.String("database", "", "existing local SQLite database (use the native binary on its host OS)")
	bundlePath := flags.String("file", "", "installation-encrypted recovery bundle")
	apply := flags.Bool("apply", false, "apply the validated changes; default is read-only validation")
	remove := flags.Bool("remove", false, "remove only the exact records in this bundle for rollback")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *database == "" || *bundlePath == "" || flags.NArg() != 0 {
		return errors.New("usage: axonhub agent-recovery --database axonhub.db --file recovery.json [--apply] [--remove]")
	}
	path, err := filepath.Abs(*database)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("an existing regular SQLite database is required")
	}
	info, err = os.Stat(*bundlePath)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return errors.New("recovery bundle is unavailable or exceeds 4 MiB")
	}
	data, err := os.ReadFile(*bundlePath)
	if err != nil {
		return err
	}
	var bundle orchestrator.AgentMessageRecoveryBundle
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return errors.New("invalid recovery bundle JSON")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("unexpected data after recovery bundle")
	}
	mode := "ro"
	if *apply {
		mode = "rw"
	}
	// Opening an existing database directly avoids server startup, automatic
	// migrations, configuration changes and background cleanup jobs.
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	location := url.URL{Scheme: "file", Path: uriPath}
	location.RawQuery = url.Values{"mode": []string{mode}, "_pragma": []string{"busy_timeout(5000)"}}.Encode()
	client, err := ent.Open("sqlite3", location.String())
	if err != nil {
		return err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	count, err := orchestrator.ImportAgentMessageRecoveries(ctx, client, bundle, *apply, *remove)
	if err != nil {
		return err
	}
	action := "validated (read-only)"
	if *apply {
		action = "applied"
	}
	fmt.Printf("Agent message recovery %s: %d record(s); original history unchanged.\n", action, count)
	return nil
}

package app

import (
	"flag"
	"fmt"
	"os"
)

// cli_share.go is `tjek share`: share a project through a file, join one,
// leave one, list them, and sync them now (sharedproject.go).

const shareUsage = `usage: tjek share                            list the shared projects
       tjek share start <project> <folder>   share a project in a new file in the folder
       tjek share join <file> [--merge]      join the project a .tjek file holds
       tjek share leave <project>            leave a project and remove its tasks here
       tjek share sync                       sync every shared project now`

func cliShare(args []string) int {
	fs := flag.NewFlagSet("share", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	merge := fs.Bool("merge", false, "join: share this device's tasks already in a project of the same name")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, shareUsage)
		fs.PrintDefaults()
	}
	flagArgs, positionals := splitFlagsAndPositionals(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	verb := "list"
	if len(positionals) > 0 {
		verb, positionals = positionals[0], positionals[1:]
	}
	want := map[string]int{"list": 0, "start": 2, "join": 1, "leave": 1, "sync": 0}
	n, known := want[verb]
	if !known || len(positionals) != n {
		fs.Usage()
		return 2
	}

	settings, sErr := loadSettings()
	if sErr != nil {
		fmt.Fprintf(os.Stderr, "warning: %v (using defaults)\n", sErr)
	}
	biases := biasesFromSettings(settings)
	c, err := loadSharedConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tjek share: %v\n", err)
		return 1
	}
	if err := openStore(); err != nil {
		fmt.Fprintf(os.Stderr, "tjek share: open store: %v\n", err)
		return 1
	}

	switch verb {
	case "list":
		if len(c.Projects) == 0 {
			fmt.Println("(no shared projects; tjek share start <project> <folder> shares one)")
			return 0
		}
		for _, p := range c.Projects {
			fmt.Printf("%s\t%s\n", p.Name, p.File)
		}
		return 0

	case "sync":
		if _, err := syncAllShared(db, biases); err != nil {
			fmt.Fprintf(os.Stderr, "tjek share: %v\n", err)
			return 1
		}
		fmt.Printf("synced %d shared project(s)\n", len(c.Projects))
		return 0

	case "leave":
		p, n, err := leaveShared(db, &c, positionals[0], biases)
		if err == nil {
			err = saveSharedConfig(c)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "tjek share: %v\n", err)
			return 1
		}
		fmt.Printf("left %q and removed its %d task(s) from this device; joining again brings them back\n", p.Name, n)
		return 0
	}

	var p sharedProject
	if verb == "start" {
		p, err = startSharing(&c, positionals[0], positionals[1])
	} else {
		p, err = joinShared(&c, positionals[0])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "tjek share: %v\n", err)
		return 1
	}
	if verb == "join" && !*merge {
		// Joining hands every task already filed under the name to everyone
		// sharing the file, and nothing takes that back, so it is asked for.
		todos, err := loadTodosFromDB(db)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tjek share: %v\n", err)
			return 1
		}
		if n := localProjectTasks(todos, p.Name); n > 0 {
			fmt.Fprintf(os.Stderr, "tjek share: you already have %d task(s) in a project named %q; joining shares them with everyone sharing it.\nRun again with --merge to go ahead.\n", n, p.Name)
			return 2
		}
	}
	if err := saveSharedConfig(c); err != nil {
		fmt.Fprintf(os.Stderr, "tjek share: %v\n", err)
		return 1
	}
	if _, err := syncShared(db, p, biases); err != nil {
		fmt.Fprintf(os.Stderr, "tjek share: %v\n", err)
		return 1
	}
	if verb == "start" {
		fmt.Printf("sharing %q in %s\n", p.Name, p.File)
	} else {
		fmt.Printf("joined %q from %s\n", p.Name, p.File)
	}
	return 0
}

// maybeSharedSyncCLI brings the shared projects up to date after a command
// that changed tasks, so a change made from the shell reaches the folder
// without the app open. Local files only, so it is quick; a failure is a
// warning, since the command itself succeeded.
func maybeSharedSyncCLI() {
	c, err := loadSharedConfig()
	if err != nil || len(c.Projects) == 0 {
		return
	}
	if err := openStore(); err != nil {
		return
	}
	settings, _ := loadSettings()
	if _, err := syncAllShared(db, biasesFromSettings(settings)); err != nil {
		fmt.Fprintf(os.Stderr, "warning: shared project sync: %v\n", err)
	}
}

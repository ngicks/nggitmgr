package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math"
	"math/rand/v2"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/ngicks/go-iterator-helper/hiter/stringsiter"
	"github.com/ngicks/go-iterator-helper/x/exp/xiter"
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}

var (
	dir = flag.String("dir", "", "base dir. can be overridden by $GITREPO_DIR. otherwise default value $HOME/gitrepo is used.s")
	tmp = flag.Bool("tmp", false, "clones in temporary mode.")
	env = flag.String("env", "", "additional env. comma-separated key=value pairs.")
)

func main() {
	flag.Parse()

	if *dir == "" {
		*dir = os.Getenv("GITREPO_DIR")
		if *dir == "" {
			*dir = filepath.Join(must(os.UserHomeDir()), "gitrepo")
		}
	}

	if _, err := os.Stat(filepath.Dir(*dir)); err != nil {
		panic(fmt.Errorf("stat parent of storage directory: %w", err))
	}

	if len(flag.Args()) != 2 {
		panic("cli arg must exactly be 1.")
	}

	if flag.Arg(0) != "clone" {
		panic("wrong command: currently only supported is clone")
	}

	tgt, err := url.Parse(flag.Arg(1))
	if err != nil {
		panic(fmt.Errorf("parsing target url: %w", err))
	}

	var tgtPath string
	if *tmp {
		made := false
		for i := 0; i < 1000; i++ {
			base := path.Base(tgt.Path) + "-" + strconv.FormatUint(uint64(rand.N[uint32](math.MaxUint32)), 10)
			tgtPath = filepath.Join(os.TempDir(), base)
			err := os.Mkdir(tgtPath, fs.ModePerm)
			if err == nil {
				made = true
				break
			}
			if !errors.Is(err, fs.ErrExist) {
				panic(err)
			}
		}
		if !made {
			panic("max attempt exceeded")
		}
	} else {
		tgtPathComponents := []string{*dir}

		if strings.Contains(tgt.Host, ":") {
			// windows rejects `:` as directory name.
			host, port, err := net.SplitHostPort(tgt.Host)
			if err != nil {
				panic(err)
			}
			tgtPathComponents = append(tgtPathComponents, host, port)
		} else {
			tgtPathComponents = append(tgtPathComponents, tgt.Host)
		}

		tgtPathComponents = append(tgtPathComponents, strings.TrimSuffix(filepath.FromSlash(tgt.Path), ".git"))

		tgtPath = filepath.Join(tgtPathComponents...)
		err = os.MkdirAll(tgtPath, fs.ModePerm)
		if err != nil {
			panic(fmt.Errorf("mkdir target path: %w", err))
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	r := runner{
		ctx:     ctx,
		dir:     tgtPath,
		stdin:   os.Stdin,
		stdout:  os.Stdout,
		stderr:  os.Stderr,
		baseCmd: "git",
		env: slices.Collect(
			xiter.Filter(func(s string) bool { return s != "" },
				stringsiter.SplitFunc(
					*env,
					-1,
					func(s string) (tokUntil int, skipUntil int) {
						i := strings.Index(s, ",")
						return i, i + 1
					},
				),
			),
		),
	}

	err = r.Run("clone", tgt.String(), ".")
	if err != nil {
		panic(err)
	}
}

type runner struct {
	ctx     context.Context
	dir     string
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
	baseCmd string
	env     []string
}

func (r runner) Run(args ...string) error {
	return r.Output(io.Discard, args...)
}

func (r runner) Output(out io.Writer, args ...string) error {
	cmd := exec.CommandContext(r.ctx, r.baseCmd, args...)
	if len(r.env) > 0 {
		cmd.Env = append(os.Environ(), r.env...)
	}
	cmd.Dir = r.dir
	cmd.Stdin = r.stdin
	cmd.Stdout = io.MultiWriter(out, r.stdout)
	cmd.Stderr = r.stderr
	return cmd.Run()
}

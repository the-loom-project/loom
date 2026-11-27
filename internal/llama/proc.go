package llama

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"loom/internal/store"
)

type State int

const (
	Loading State = iota
	Ready
	Stopped
	Failed
)

func (s State) String() string {
	return [...]string{"loading", "ready", "stopped", "failed"}[s]
}

type Instance struct {
	ID      int
	Model   store.Model
	Config  Config
	Command Command
	Port    int
	Started time.Time

	mu    sync.Mutex
	state State
	err   error
	log   []string
	cmd   *exec.Cmd
	stop  bool
}

const maxLog = 4000

func (i *Instance) URL() string { return fmt.Sprintf("http://127.0.0.1:%d", i.Port) }

func (i *Instance) State() (State, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.state, i.err
}

func (i *Instance) Log() []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append([]string(nil), i.log...)
}

func Start(id int, e Env, cfg Config, cmd Command, port int) (*Instance, error) {
	inst := &Instance{ID: id, Model: e.Model, Config: cfg, Command: cmd, Port: port, Started: time.Now()}
	c := exec.Command(cmd.Bin, cmd.Argv()...)
	c.Dir = cmd.Dir
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	c.Stdout, c.Stderr = pw, pw
	if err := c.Start(); err != nil {
		pr.Close()
		pw.Close()
		return nil, err
	}
	pw.Close()
	inst.cmd = c

	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			inst.mu.Lock()
			inst.log = append(inst.log, strings.TrimRight(sc.Text(), "\r"))
			if len(inst.log) > maxLog {
				inst.log = inst.log[len(inst.log)-maxLog:]
			}
			inst.mu.Unlock()
		}
		if err := sc.Err(); err != nil {
			inst.mu.Lock()
			inst.log = append(inst.log, "loom stopped reading the log: "+err.Error())
			inst.mu.Unlock()
		}
	}()
	go func() {
		err := c.Wait()
		inst.mu.Lock()
		defer inst.mu.Unlock()
		switch {
		case inst.stop || err == nil:
			inst.state = Stopped
		default:
			inst.state, inst.err = Failed, err
		}
	}()
	go inst.waitReady()
	return inst, nil
}

func (i *Instance) waitReady() {
	url := i.URL() + "/health"
	for {
		time.Sleep(400 * time.Millisecond)
		i.mu.Lock()
		done := i.state == Stopped || i.state == Failed
		i.mu.Unlock()
		if done {
			return
		}
		resp, err := http.Get(url)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			i.mu.Lock()
			if i.state == Loading {
				i.state = Ready
			}
			i.mu.Unlock()
			return
		}
	}
}

func (i *Instance) Stop() {
	i.mu.Lock()
	if i.state == Stopped || i.state == Failed || i.stop {
		i.mu.Unlock()
		return
	}
	i.stop = true
	pid := i.cmd.Process.Pid
	i.mu.Unlock()
	syscall.Kill(-pid, syscall.SIGTERM)
	time.AfterFunc(5*time.Second, func() { syscall.Kill(-pid, syscall.SIGKILL) })
}

func (i *Instance) Alive() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.state == Loading || i.state == Ready
}

func FreePort(host string, want int) int {
	for p := want; p < want+100; p++ {
		l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, p))
		if err == nil {
			l.Close()
			return p
		}
	}
	return want
}

func (c Command) Exec() *exec.Cmd {
	cmd := exec.Command(c.Bin, c.Argv()...)
	cmd.Dir = c.Dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd
}

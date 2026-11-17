package docker

import (
	"bytes"
	"fmt"
	"os/exec"
)

type Compose struct {
	Dir string
}

func NewCompose(dir string) *Compose {
	return &Compose{Dir: dir}
}

func (c *Compose) Run(args ...string) (string, error) {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = c.Dir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	return out.String(), err
}

func (c *Compose) Exec(service string, cmd ...string) error {
	args := append([]string{"exec", "-T", service}, cmd...)
	out, err := c.Run(args...)
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

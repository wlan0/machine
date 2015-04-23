package provision

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"text/template"

	"github.com/docker/machine/drivers"
	"github.com/docker/machine/libmachine/auth"
	"github.com/docker/machine/libmachine/engine"
	"github.com/docker/machine/libmachine/provision/pkgaction"
	"github.com/docker/machine/libmachine/swarm"
	"github.com/docker/machine/log"
	"github.com/docker/machine/ssh"
)

const (
	rancherTmpl = `#cloud-config

rancher:
  user_docker:
    tls: true
    tls_args: 
     - --tlsverify
     - --tlscacert={{ .CaCertPath }}
     - --tlscert={{ .ServerCertPath }} 
     - --tlskey={{ .ServerKeyPath }}
     - '-H=0.0.0.0:{{ .DockerPort }}'`
)

var (
	ErrUnsupportedAction = errors.New("unsupported action")
)

type RancherConfig struct {
	CaCertPath     string
	ServerCertPath string
	ServerKeyPath  string
	DockerPort     int
}

func init() {
	Register("rancheros", &RegisteredProvisioner{
		New: NewRancherProvisioner,
	})
}

func NewRancherProvisioner(d drivers.Driver) Provisioner {
	return &RancherProvisioner{
		Driver: d,
	}
}

type RancherProvisioner struct {
	OsReleaseInfo *OsRelease
	Driver        drivers.Driver
	SwarmOptions  swarm.SwarmOptions
	AuthOptions   auth.AuthOptions
	EngineOptions engine.EngineOptions
}

func (provisioner *RancherProvisioner) GetAuthOptions() auth.AuthOptions {
	return provisioner.AuthOptions
}

func (provisioner *RancherProvisioner) Service(name string, action pkgaction.ServiceAction) error {
	var err error

	switch action {
	case pkgaction.Start:
		_, err = provisioner.SSHCommand(`sudo system-docker run --net host --label io.rancher.os.scope=system --volumes-from command-volumes --volumes-from system-volumes cloudinit
sudo system-docker restart userdocker`)
	case pkgaction.Stop:
		_, err = provisioner.SSHCommand("sudo system-docker stop userdocker")
	case pkgaction.Restart:
		var out ssh.Output
		out, err = provisioner.SSHCommand("sudo reboot")
		debugSSHCommand(out, "restart")
	}

	if err != nil && action != pkgaction.Restart {
		return err
	}

	return nil
}

func (provisioner *RancherProvisioner) Package(name string, action pkgaction.PackageAction) error {

	switch action {
	case pkgaction.Upgrade:
		out, _ := provisioner.SSHCommand("(echo y; echo n;) | sudo rancherctl os upgrade")
		debugSSHCommand(out, "upgrade")
	default:
		return ErrUnsupportedAction
	}
	return nil
}

func debugSSHCommand(out ssh.Output, command string) {
	if out.Stdout == nil || out.Stderr == nil {
		return
	}
	var se, so bytes.Buffer
	se.ReadFrom(out.Stderr)
	so.ReadFrom(out.Stdout)

	log.Debugf("%s stdout : %s", command, so.String())
	log.Debugf("%s stderr : %s", command, se.String())
}

func (provisioner *RancherProvisioner) Hostname() (string, error) {
	out, err := provisioner.SSHCommand(fmt.Sprintf("hostname"))
	if err != nil {
		return "", err
	}

	var so bytes.Buffer
	if _, err := so.ReadFrom(out.Stdout); err != nil {
		return "", err
	}

	return so.String(), nil
}

func (provisioner *RancherProvisioner) SetHostname(hostname string) error {
	_, err := provisioner.SSHCommand(fmt.Sprintf(`
sudo mkdir -p /var/lib/rancher/conf/cloud-config.d/  
sudo tee /var/lib/rancher/conf/cloud-config.d/machine-hostname.yml << EOF
#cloud-config

hostname: %s
EOF
sudo tee /etc/hostname << EOF
%s
EOF
sudo tee -a /etc/hosts << EOF
127.0.0.1 %s
EOF
sudo hostname -F /etc/hostname
sudo chmod 0600 /var/lib/rancher/conf/cloud-config.d/machine-hostname.yml
sudo chmod 0644 /etc/hostname
sudo chmod 0644 /etc/hosts
`,
		hostname, hostname, hostname,
	))

	return err
}

func (provisioner *RancherProvisioner) GetDockerOptionsDir() string {
	return "/var/lib/rancher"
}

func (provisioner *RancherProvisioner) GenerateDockerOptions(dockerPort int) (*DockerOptions, error) {
	var buf bytes.Buffer
	authOptions := provisioner.AuthOptions
	cfg := &RancherConfig{
		CaCertPath:     authOptions.CaCertRemotePath,
		ServerCertPath: authOptions.ServerCertRemotePath,
		ServerKeyPath:  authOptions.ServerKeyRemotePath,
		DockerPort:     dockerPort,
	}

	t := template.Must(template.New("rancher").Parse(rancherTmpl))
	t.Execute(&buf, cfg)
	daemonCfg := buf.String()
	daemonOptsDir := path.Join(provisioner.GetDockerOptionsDir(), "conf/cloud-config.d", "docker-config.yml")
	return &DockerOptions{
		EngineOptions:     daemonCfg,
		EngineOptionsPath: daemonOptsDir,
	}, nil
}

func (provisioner *RancherProvisioner) CompatibleWithHost() bool {
	return provisioner.OsReleaseInfo.Id == "rancheros"
}

func (provisioner *RancherProvisioner) SetOsReleaseInfo(info *OsRelease) {
	provisioner.OsReleaseInfo = info
}

func (provisioner *RancherProvisioner) Provision(swarmOptions swarm.SwarmOptions, authOptions auth.AuthOptions, engineOptions engine.EngineOptions) error {
	if err := provisioner.SetHostname(provisioner.Driver.GetMachineName()); err != nil {
		return err
	}

	provisioner.AuthOptions = authOptions
	provisioner.EngineOptions = engineOptions

	if err := installDockerGeneric(provisioner); err != nil {
		return err
	}

	provisioner.AuthOptions = setRemoteAuthOptions(provisioner)

	if err := ConfigureAuth(provisioner); err != nil {
		return err
	}

	if err := configureSwarm(provisioner, swarmOptions); err != nil {
		return err
	}

	return nil
}

func (provisioner *RancherProvisioner) SSHCommand(command string) (ssh.Output, error) {
	return drivers.RunSSHCommandFromDriver(provisioner.Driver, command)
}

func (provisioner *RancherProvisioner) GetDriver() drivers.Driver {
	return provisioner.Driver
}

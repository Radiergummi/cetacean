// Package compose renders live Swarm specs as a compose document. The promise
// is a file that redeploys to the same running state on the same cluster, not
// one byte-identical to whatever created the stack.
package compose

// File is a compose document. Every map renders in sorted key order.
type File struct {
	Services map[string]Service     `yaml:"services,omitempty"`
	Networks map[string]Network     `yaml:"networks,omitempty"`
	Volumes  map[string]Volume      `yaml:"volumes,omitempty"`
	Configs  map[string]ExternalRef `yaml:"configs,omitempty"`
	Secrets  map[string]ExternalRef `yaml:"secrets,omitempty"`
}

// ExternalRef is a config, secret, network or volume the deploy must not
// create. Name is always stated, even when it equals the key: it is what
// keeps the reference resolving if the file is deployed under another
// project name.
type ExternalRef struct {
	External bool   `yaml:"external"`
	Name     string `yaml:"name,omitempty"`
}

type Service struct {
	Image       string             `yaml:"image,omitempty"`
	Hostname    string             `yaml:"hostname,omitempty"`
	Command     []string           `yaml:"command,omitempty"`
	Entrypoint  []string           `yaml:"entrypoint,omitempty"`
	WorkingDir  string             `yaml:"working_dir,omitempty"`
	User        string             `yaml:"user,omitempty"`
	GroupAdd    []string           `yaml:"group_add,omitempty"`
	Environment map[string]*string `yaml:"environment,omitempty"`
	Labels      map[string]string  `yaml:"labels,omitempty"`
	ExtraHosts  []string           `yaml:"extra_hosts,omitempty"`
	Networks    []string           `yaml:"networks,omitempty"`
	Volumes     []ServiceVolume    `yaml:"volumes,omitempty"`
	Ports       []ServicePort      `yaml:"ports,omitempty"`
	Configs     []FileRef          `yaml:"configs,omitempty"`
	Secrets     []FileRef          `yaml:"secrets,omitempty"`
	Healthcheck *Healthcheck       `yaml:"healthcheck,omitempty"`
	Logging     *Logging           `yaml:"logging,omitempty"`
	StopSignal  string             `yaml:"stop_signal,omitempty"`
	StopGrace   string             `yaml:"stop_grace_period,omitempty"`
	Init        *bool              `yaml:"init,omitempty"`
	TTY         bool               `yaml:"tty,omitempty"`
	StdinOpen   bool               `yaml:"stdin_open,omitempty"`
	ReadOnly    bool               `yaml:"read_only,omitempty"`
	Privileged  bool               `yaml:"privileged,omitempty"`
	CapAdd      []string           `yaml:"cap_add,omitempty"`
	CapDrop     []string           `yaml:"cap_drop,omitempty"`
	SecurityOpt []string           `yaml:"security_opt,omitempty"`
	CredSpec    map[string]string  `yaml:"credential_spec,omitempty"`
	Sysctls     map[string]string  `yaml:"sysctls,omitempty"`
	DNS         []string           `yaml:"dns,omitempty"`
	DNSSearch   []string           `yaml:"dns_search,omitempty"`
	Isolation   string             `yaml:"isolation,omitempty"`
	Platform    string             `yaml:"platform,omitempty"`
	OomScoreAdj int64              `yaml:"oom_score_adj,omitempty"`
	Deploy      *Deploy            `yaml:"deploy,omitempty"`
}

// Deploy carries what Swarm owns and a plain container runtime does not.
type Deploy struct {
	Mode           string            `yaml:"mode,omitempty"`
	Replicas       *uint64           `yaml:"replicas,omitempty"`
	Labels         map[string]string `yaml:"labels,omitempty"`
	EndpointMode   string            `yaml:"endpoint_mode,omitempty"`
	Resources      *Resources        `yaml:"resources,omitempty"`
	Placement      *Placement        `yaml:"placement,omitempty"`
	UpdateConfig   *UpdateConfig     `yaml:"update_config,omitempty"`
	RollbackConfig *UpdateConfig     `yaml:"rollback_config,omitempty"`
	RestartPolicy  *RestartPolicy    `yaml:"restart_policy,omitempty"`
}

type Resources struct {
	Limits       *ResourceLimit `yaml:"limits,omitempty"`
	Reservations *ResourceLimit `yaml:"reservations,omitempty"`
}

// ResourceLimit holds compose's string forms: "0.50" cores, "512M" bytes.
type ResourceLimit struct {
	CPUs   string `yaml:"cpus,omitempty"`
	Memory string `yaml:"memory,omitempty"`
	PIDs   int64  `yaml:"pids,omitempty"`
}

type Placement struct {
	Constraints []string            `yaml:"constraints,omitempty"`
	Preferences []map[string]string `yaml:"preferences,omitempty"`
	MaxReplicas uint64              `yaml:"max_replicas_per_node,omitempty"`
}

type UpdateConfig struct {
	Parallelism     *uint64 `yaml:"parallelism,omitempty"`
	Delay           string  `yaml:"delay,omitempty"`
	FailureAction   string  `yaml:"failure_action,omitempty"`
	Monitor         string  `yaml:"monitor,omitempty"`
	MaxFailureRatio float32 `yaml:"max_failure_ratio,omitempty"`
	Order           string  `yaml:"order,omitempty"`
}

type RestartPolicy struct {
	Condition   string  `yaml:"condition,omitempty"`
	Delay       string  `yaml:"delay,omitempty"`
	MaxAttempts *uint64 `yaml:"max_attempts,omitempty"`
	Window      string  `yaml:"window,omitempty"`
}

type Healthcheck struct {
	Test          []string `yaml:"test,omitempty"`
	Interval      string   `yaml:"interval,omitempty"`
	Timeout       string   `yaml:"timeout,omitempty"`
	Retries       int      `yaml:"retries,omitempty"`
	StartPeriod   string   `yaml:"start_period,omitempty"`
	StartInterval string   `yaml:"start_interval,omitempty"`
}

type Logging struct {
	Driver  string            `yaml:"driver,omitempty"`
	Options map[string]string `yaml:"options,omitempty"`
}

// ServiceVolume is compose's long mount syntax.
type ServiceVolume struct {
	Type     string `yaml:"type"`
	Source   string `yaml:"source,omitempty"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"read_only,omitempty"`
}

// ServicePort is compose's long port syntax.
type ServicePort struct {
	Target    uint32 `yaml:"target"`
	Published uint32 `yaml:"published,omitempty"`
	Protocol  string `yaml:"protocol,omitempty"`
	Mode      string `yaml:"mode,omitempty"`
}

// FileRef is a config or secret mounted into a service.
type FileRef struct {
	Source string `yaml:"source"`
	Target string `yaml:"target,omitempty"`
	UID    string `yaml:"uid,omitempty"`
	GID    string `yaml:"gid,omitempty"`
	Mode   uint32 `yaml:"mode,omitempty"`
}

type Network struct {
	External   bool              `yaml:"external,omitempty"`
	Name       string            `yaml:"name,omitempty"`
	Driver     string            `yaml:"driver,omitempty"`
	DriverOpts map[string]string `yaml:"driver_opts,omitempty"`
	Attachable bool              `yaml:"attachable,omitempty"`
	Internal   bool              `yaml:"internal,omitempty"`
	EnableIPv6 bool              `yaml:"enable_ipv6,omitempty"`
	Labels     map[string]string `yaml:"labels,omitempty"`
}

type Volume struct {
	External   bool              `yaml:"external,omitempty"`
	Name       string            `yaml:"name,omitempty"`
	Driver     string            `yaml:"driver,omitempty"`
	DriverOpts map[string]string `yaml:"driver_opts,omitempty"`
	Labels     map[string]string `yaml:"labels,omitempty"`
}

import type { Config, Network, Node, Secret, Service, Task, Volume } from "@/api/types";

// Object IDs — 25-char hex strings matching Docker's format.
const idNodeManager1 = "e75ddf4dc6ec154106fd4a313";
const idNodeWorker1 = "cda13933d6f1050f77e9597b4";
const idNodeWorker2 = "940d7372c887e1e633cc88b58";

const idNetIngress = "8f06aabea7e445e5559d534ea";
const idNetWebshop = "b8c221ffaaf4e894d26b2de27";
const idNetMonitoring = "70f83ede9b35ce04785f91c55";
const idNetInfra = "6ff411ff9b9e506b2a0bcea53";
const idNetGwbridge = "53699df749a632ed1271418bb";

const idSvcFrontend = "3b8528a77abd8db4aba61847d";
const idSvcAPI = "21d84f504ed921fc6a8a59cf2";
const idSvcWorker = "7a1b408798de674daacfc3019";
const idSvcDB = "6b1dc7141258bcdbab88eaf99";
const idSvcCache = "b7228fb632f76183dde44b3e3";
const idSvcSearch = "3b5c112667ae30503120919a1";
const idSvcPrometheus = "9bbeee3828003d356f192c2a2";
const idSvcGrafana = "b638ce33acb6d622b3b1616e9";
const idSvcNodeExporter = "2117d97bea10551cfd739740a";
const idSvcProxy = "ee4ec09a6befe327ed9475733";
const idSvcRegistry = "6f0aaf260e5814212041e2dc2";

const idCfgDBInit = "f7a9daa1067b5ff691de451a9";
const idCfgPromConfig = "fea23f1dd15e59fccfec21a4a";
const idCfgProxyConf = "acd87c5573681ea3283f22885";

const idSecDBPassword = "d50d95742b9a2cdcc913b5a72";
const idSecAPIKey = "a9688d6e8a5fc08659708e6f0";

const idSwarm = "521bb79758f0e209fba51c775";

// Image digests.
const digestNginx = "sha256:c12623164b8bd229b3ccea41cc8dab591569b681157b598cf16cb742dea3a32e";
const digestWebshopAPI = "sha256:2ff9a84c8d762f302022090147cdc04374aa3adff1e244cc8ffa50391496b8ee";
const digestWebshopWorker =
  "sha256:9468e9be53873fbf5b6871c060b2bdd354b14897887ed8ea1f4d69a9ef0f8df5";
const digestPostgres = "sha256:10ba9412b90e1f5ccd1a340a5199c61f56fa05cc5b803aa58c20a2def92caa64";
const digestRedis = "sha256:6b8aa430c358736426a31de605f28bf2abdce848f6cc453ed009e1c620255eff";
const digestElastic = "sha256:68db44c1d00f133b6e19e4c969284e68f6584f3b724bfb005e18b7e7a7cf0d82";
const digestPrometheus = "sha256:0f7683f7e8bca879cf8967dab246204355194007f14b1c7b0332a70c3249592c";
const digestGrafana = "sha256:d8bf37f634da1ea4883fb3e219ffeead203ca8733ee10c3eb8582e6bb04d3d75";
const digestNodeExporter =
  "sha256:1046a6cf60cd119ebdb992b87c5b59ebf0643fe0a81080c612435d05476a77fe";
const digestTraefik = "sha256:b13aa4bf4e0cd52de7ebe92a30e5537b0ee6406a1451b314047e27c99942e472";
const digestRegistry = "sha256:38dffaed02256502ace095c85f6ed8b9c5a637769af8102ab2901c90d5b66fbf";

const now = new Date();
const ago4d = new Date(now.getTime() - 4 * 24 * 60 * 60 * 1000);
const ago3d = new Date(now.getTime() - 3 * 24 * 60 * 60 * 1000);
const ago2h = new Date(now.getTime() - 2 * 60 * 60 * 1000);
const ago1h = new Date(now.getTime() - 1 * 60 * 60 * 1000);
const ago30m = new Date(now.getTime() - 30 * 60 * 1000);

export interface SwarmData {
  ID: string;
  CreatedAt: string;
  UpdatedAt: string;
  Spec: {
    Annotations: { Name: string; Labels: Record<string, string> };
    Orchestration: { TaskHistoryRetentionLimit: number };
    Raft: {
      SnapshotInterval: number;
      KeepOldSnapshots: number;
      LogEntriesForSlowFollowers: number;
      ElectionTick: number;
      HeartbeatTick: number;
    };
    Dispatcher: { HeartbeatPeriod: number };
    CAConfig: { NodeCertExpiry: number; ForceRotate: number };
    TaskDefaults: Record<string, never>;
    EncryptionConfig: { AutoLockManagers: boolean };
  };
  TLSInfo: {
    TrustRoot: string;
    CertIssuerSubject: string;
    CertIssuerPublicKey: string;
  };
  RootRotationInProgress: boolean;
  DefaultAddrPool: string[] | null;
  SubnetSize: number;
  DataPathPort: number;
  JoinTokens: { Worker: string; Manager: string };
}

export interface Dataset {
  nodes: Node[];
  services: Service[];
  tasks: Task[];
  configs: Config[];
  secrets: Secret[];
  networks: Network[];
  volumes: Volume[];
  swarm: SwarmData;

  nodesByID: Map<string, Node>;
  servicesByID: Map<string, Service>;
  tasksByID: Map<string, Task>;
  configsByID: Map<string, Config>;
  secretsByID: Map<string, Secret>;
  networksByID: Map<string, Network>;
  volumesByName: Map<string, Volume>;
}

function buildNodes(): Node[] {
  return [
    {
      ID: idNodeManager1,
      Version: { Index: 10 },
      Spec: {
        Role: "manager",
        Availability: "active",
        Labels: null,
      },
      Description: {
        Hostname: "swarm-manager-1",
        Platform: { Architecture: "x86_64", OS: "linux" },
        Resources: { NanoCPUs: 8_000_000_000, MemoryBytes: 16 * 1024 * 1024 * 1024 },
        Engine: { EngineVersion: "27.5.1" },
      },
      Status: { State: "ready", Addr: "10.0.0.1" },
      ManagerStatus: { Leader: true, Reachability: "reachable", Addr: "10.0.0.1:2377" },
    },
    {
      ID: idNodeWorker1,
      Version: { Index: 11 },
      Spec: {
        Role: "worker",
        Availability: "active",
        Labels: null,
      },
      Description: {
        Hostname: "swarm-worker-1",
        Platform: { Architecture: "x86_64", OS: "linux" },
        Resources: { NanoCPUs: 4_000_000_000, MemoryBytes: 8 * 1024 * 1024 * 1024 },
        Engine: { EngineVersion: "27.5.1" },
      },
      Status: { State: "ready", Addr: "10.0.0.2" },
    },
    {
      ID: idNodeWorker2,
      Version: { Index: 12 },
      Spec: {
        Role: "worker",
        Availability: "active",
        Labels: null,
      },
      Description: {
        Hostname: "swarm-worker-2",
        Platform: { Architecture: "x86_64", OS: "linux" },
        Resources: { NanoCPUs: 4_000_000_000, MemoryBytes: 8 * 1024 * 1024 * 1024 },
        Engine: { EngineVersion: "27.5.1" },
      },
      Status: { State: "ready", Addr: "10.0.0.3" },
    },
  ];
}

function buildNetworks(): Network[] {
  return [
    {
      Id: idNetIngress,
      Name: "ingress",
      Created: ago4d.toISOString(),
      Driver: "overlay",
      Scope: "swarm",
      EnableIPv6: false,
      Internal: false,
      Attachable: false,
      Ingress: true,
      IPAM: { Driver: "default", Config: [{ Subnet: "10.0.0.0/24", Gateway: "10.0.0.1" }] },
      Options: null,
      Labels: null,
    },
    {
      Id: idNetWebshop,
      Name: "webshop_default",
      Created: ago4d.toISOString(),
      Driver: "overlay",
      Scope: "swarm",
      EnableIPv6: false,
      Internal: false,
      Attachable: false,
      Ingress: false,
      IPAM: { Driver: "default", Config: [{ Subnet: "10.0.1.0/24", Gateway: "10.0.1.1" }] },
      Options: null,
      Labels: { "com.docker.stack.namespace": "webshop" },
    },
    {
      Id: idNetMonitoring,
      Name: "monitoring_default",
      Created: ago4d.toISOString(),
      Driver: "overlay",
      Scope: "swarm",
      EnableIPv6: false,
      Internal: false,
      Attachable: false,
      Ingress: false,
      IPAM: { Driver: "default", Config: [{ Subnet: "10.0.2.0/24", Gateway: "10.0.2.1" }] },
      Options: null,
      Labels: { "com.docker.stack.namespace": "monitoring" },
    },
    {
      Id: idNetInfra,
      Name: "infra_default",
      Created: ago4d.toISOString(),
      Driver: "overlay",
      Scope: "swarm",
      EnableIPv6: false,
      Internal: false,
      Attachable: false,
      Ingress: false,
      IPAM: { Driver: "default", Config: [{ Subnet: "10.0.3.0/24", Gateway: "10.0.3.1" }] },
      Options: null,
      Labels: { "com.docker.stack.namespace": "infra" },
    },
    {
      Id: idNetGwbridge,
      Name: "docker_gwbridge",
      Created: ago4d.toISOString(),
      Driver: "bridge",
      Scope: "local",
      EnableIPv6: false,
      Internal: false,
      Attachable: false,
      Ingress: false,
      IPAM: { Driver: "default", Config: [{ Subnet: "172.18.0.0/16", Gateway: "172.18.0.1" }] },
      Options: null,
      Labels: null,
    },
  ];
}

function buildVolumes(): Volume[] {
  return [
    {
      Name: "webshop_db-data",
      Driver: "local",
      Scope: "local",
      Mountpoint: "/var/lib/docker/volumes/webshop_db-data/_data",
      Labels: { "com.docker.stack.namespace": "webshop" },
      Options: {},
      CreatedAt: ago4d.toISOString(),
    },
    {
      Name: "monitoring_prometheus-data",
      Driver: "local",
      Scope: "local",
      Mountpoint: "/var/lib/docker/volumes/monitoring_prometheus-data/_data",
      Labels: { "com.docker.stack.namespace": "monitoring" },
      Options: {},
      CreatedAt: ago4d.toISOString(),
    },
  ];
}

function buildSwarm(): SwarmData {
  return {
    ID: idSwarm,
    CreatedAt: ago4d.toISOString(),
    UpdatedAt: ago3d.toISOString(),
    Spec: {
      Annotations: { Name: "default", Labels: {} },
      Orchestration: { TaskHistoryRetentionLimit: 5 },
      Raft: {
        SnapshotInterval: 10000,
        KeepOldSnapshots: 0,
        LogEntriesForSlowFollowers: 500,
        ElectionTick: 10,
        HeartbeatTick: 1,
      },
      Dispatcher: { HeartbeatPeriod: 5_000_000_000 },
      CAConfig: { NodeCertExpiry: 7776000_000_000_000, ForceRotate: 0 },
      TaskDefaults: {},
      EncryptionConfig: { AutoLockManagers: false },
    },
    TLSInfo: { TrustRoot: "", CertIssuerSubject: "", CertIssuerPublicKey: "" },
    RootRotationInProgress: false,
    DefaultAddrPool: null,
    SubnetSize: 24,
    DataPathPort: 4789,
    JoinTokens: {
      Worker: "SWMTKN-1-0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a-worker000000000000000000",
      Manager: "SWMTKN-1-0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a0a-manager00000000000000000",
    },
  };
}

function buildConfigs(): Config[] {
  const dbInitSQL = `CREATE TABLE IF NOT EXISTS products (
  id SERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  price NUMERIC(10,2) NOT NULL
);

CREATE TABLE IF NOT EXISTS orders (
  id SERIAL PRIMARY KEY,
  customer_id INT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now()
);
`;

  const prometheusYAML = `global:
  scrape_interval: 15s

scrape_configs:
  - job_name: node-exporter
    dns_sd_configs:
      - names: ['tasks.node-exporter']
        type: A
        port: 9100
  - job_name: cadvisor
    dns_sd_configs:
      - names: ['tasks.cadvisor']
        type: A
        port: 8080
`;

  const proxyTOML = `[entryPoints]
  [entryPoints.web]
    address = ":80"
  [entryPoints.websecure]
    address = ":443"

[api]
  dashboard = true

[providers.docker]
  swarmMode = true
  exposedByDefault = false
`;

  return [
    {
      ID: idCfgDBInit,
      Version: { Index: 20 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago4d.toISOString(),
      Spec: {
        Name: "webshop_db-init",
        Labels: { "com.docker.stack.namespace": "webshop" },
        Data: btoa(dbInitSQL),
      },
    },
    {
      ID: idCfgPromConfig,
      Version: { Index: 21 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago3d.toISOString(),
      Spec: {
        Name: "monitoring_prometheus-config",
        Labels: { "com.docker.stack.namespace": "monitoring" },
        Data: btoa(prometheusYAML),
      },
    },
    {
      ID: idCfgProxyConf,
      Version: { Index: 22 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago3d.toISOString(),
      Spec: {
        Name: "infra_proxy-config",
        Labels: { "com.docker.stack.namespace": "infra" },
        Data: btoa(proxyTOML),
      },
    },
  ];
}

function buildSecrets(): Secret[] {
  return [
    {
      ID: idSecDBPassword,
      Version: { Index: 30 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago4d.toISOString(),
      Spec: {
        Name: "webshop_db-password",
        Labels: { "com.docker.stack.namespace": "webshop" },
      },
    },
    {
      ID: idSecAPIKey,
      Version: { Index: 31 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago4d.toISOString(),
      Spec: {
        Name: "webshop_api-key",
        Labels: { "com.docker.stack.namespace": "webshop" },
      },
    },
  ];
}

function buildServices(): Service[] {
  const webshopLabels = (image: string): Record<string, string> => ({
    "com.docker.stack.namespace": "webshop",
    "com.docker.stack.image": image,
  });

  const monitoringLabels = (image: string): Record<string, string> => ({
    "com.docker.stack.namespace": "monitoring",
    "com.docker.stack.image": image,
  });

  const infraLabels = (image: string): Record<string, string> => ({
    "com.docker.stack.namespace": "infra",
    "com.docker.stack.image": image,
  });

  return [
    // webshop_frontend
    {
      ID: idSvcFrontend,
      Version: { Index: 100 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago3d.toISOString(),
      Spec: {
        Name: "webshop_frontend",
        Labels: webshopLabels("nginx:1.27-alpine"),
        TaskTemplate: {
          ContainerSpec: {
            Image: `nginx:1.27-alpine@${digestNginx}`,
            Healthcheck: {
              Test: ["CMD", "curl", "-f", "http://localhost/"],
              Interval: 30_000_000_000,
              Timeout: 5_000_000_000,
              Retries: 3,
            },
          },
          Resources: {
            Limits: { NanoCPUs: 500_000_000, MemoryBytes: 128 * 1024 * 1024 },
            Reservations: { NanoCPUs: 100_000_000, MemoryBytes: 64 * 1024 * 1024 },
          },
          Networks: [{ Target: idNetWebshop }],
        },
        Mode: { Replicated: { Replicas: 3 } },
      },
    },

    // webshop_api
    {
      ID: idSvcAPI,
      Version: { Index: 101 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago3d.toISOString(),
      Spec: {
        Name: "webshop_api",
        Labels: webshopLabels("registry.example.com/webshop/api:v2.4.1"),
        TaskTemplate: {
          ContainerSpec: {
            Image: `registry.example.com/webshop/api:v2.4.1@${digestWebshopAPI}`,
            Env: [
              "DATABASE_URL=postgres://webshop:$(DB_PASSWORD)@webshop_db:5432/webshop",
              "REDIS_URL=redis://webshop_cache:6379",
              "LOG_LEVEL=info",
            ],
            Healthcheck: {
              Test: ["CMD", "curl", "-f", "http://localhost:8080/health"],
              Interval: 10_000_000_000,
              Timeout: 5_000_000_000,
              Retries: 3,
            },
            Secrets: [
              {
                SecretID: idSecAPIKey,
                SecretName: "webshop_api-key",
                File: { Name: "api-key", UID: "0", GID: "0", Mode: 0o444 },
              },
            ],
          },
          Resources: {
            Limits: { NanoCPUs: 1_000_000_000, MemoryBytes: 512 * 1024 * 1024 },
            Reservations: { NanoCPUs: 250_000_000, MemoryBytes: 128 * 1024 * 1024 },
          },
          Networks: [{ Target: idNetWebshop }],
        },
        Mode: { Replicated: { Replicas: 3 } },
      },
    },

    // webshop_worker
    {
      ID: idSvcWorker,
      Version: { Index: 102 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago3d.toISOString(),
      Spec: {
        Name: "webshop_worker",
        Labels: webshopLabels("registry.example.com/webshop/worker:v2.4.1"),
        TaskTemplate: {
          ContainerSpec: {
            Image: `registry.example.com/webshop/worker:v2.4.1@${digestWebshopWorker}`,
            Env: [
              "DATABASE_URL=postgres://webshop:$(DB_PASSWORD)@webshop_db:5432/webshop",
              "REDIS_URL=redis://webshop_cache:6379",
            ],
          },
          Resources: {
            Limits: { NanoCPUs: 500_000_000, MemoryBytes: 256 * 1024 * 1024 },
            Reservations: { NanoCPUs: 100_000_000, MemoryBytes: 64 * 1024 * 1024 },
          },
          Networks: [{ Target: idNetWebshop }],
        },
        Mode: { Replicated: { Replicas: 2 } },
      },
    },

    // webshop_db
    {
      ID: idSvcDB,
      Version: { Index: 103 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago4d.toISOString(),
      Spec: {
        Name: "webshop_db",
        Labels: webshopLabels("postgres:16-alpine"),
        TaskTemplate: {
          ContainerSpec: {
            Image: `postgres:16-alpine@${digestPostgres}`,
            Env: [
              "POSTGRES_DB=webshop",
              "POSTGRES_USER=webshop",
              "POSTGRES_PASSWORD_FILE=/run/secrets/db-password",
            ],
            Mounts: [
              { Type: "volume", Source: "webshop_db-data", Target: "/var/lib/postgresql/data" },
            ],
            Secrets: [
              {
                SecretID: idSecDBPassword,
                SecretName: "webshop_db-password",
                File: { Name: "db-password", UID: "0", GID: "0", Mode: 0o444 },
              },
            ],
            Configs: [
              {
                ConfigID: idCfgDBInit,
                ConfigName: "webshop_db-init",
                File: {
                  Name: "/docker-entrypoint-initdb.d/init.sql",
                  UID: "0",
                  GID: "0",
                  Mode: 0o444,
                },
              },
            ],
          },
          Resources: {
            Limits: { NanoCPUs: 1_000_000_000, MemoryBytes: 1024 * 1024 * 1024 },
            Reservations: { NanoCPUs: 500_000_000, MemoryBytes: 512 * 1024 * 1024 },
          },
          Networks: [{ Target: idNetWebshop }],
        },
        Mode: { Replicated: { Replicas: 1 } },
      },
    },

    // webshop_cache
    {
      ID: idSvcCache,
      Version: { Index: 104 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago4d.toISOString(),
      Spec: {
        Name: "webshop_cache",
        Labels: webshopLabels("redis:7-alpine"),
        TaskTemplate: {
          ContainerSpec: {
            Image: `redis:7-alpine@${digestRedis}`,
            Healthcheck: {
              Test: ["CMD", "redis-cli", "ping"],
              Interval: 10_000_000_000,
              Timeout: 5_000_000_000,
              Retries: 3,
            },
          },
          Resources: {
            Limits: { NanoCPUs: 500_000_000, MemoryBytes: 256 * 1024 * 1024 },
            Reservations: { NanoCPUs: 100_000_000, MemoryBytes: 64 * 1024 * 1024 },
          },
          Networks: [{ Target: idNetWebshop }],
        },
        Mode: { Replicated: { Replicas: 1 } },
      },
    },

    // webshop_search
    {
      ID: idSvcSearch,
      Version: { Index: 105 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago3d.toISOString(),
      Spec: {
        Name: "webshop_search",
        Labels: webshopLabels("elasticsearch:8.17.0"),
        TaskTemplate: {
          ContainerSpec: {
            Image: `elasticsearch:8.17.0@${digestElastic}`,
            Env: [
              "discovery.type=zen",
              "cluster.name=webshop-search",
              "ES_JAVA_OPTS=-Xms1g -Xmx1g",
            ],
          },
          Resources: {
            Limits: { NanoCPUs: 2_000_000_000, MemoryBytes: 2 * 1024 * 1024 * 1024 },
            Reservations: { NanoCPUs: 500_000_000, MemoryBytes: 1024 * 1024 * 1024 },
          },
          Networks: [{ Target: idNetWebshop }],
        },
        Mode: { Replicated: { Replicas: 2 } },
      },
    },

    // monitoring_prometheus
    {
      ID: idSvcPrometheus,
      Version: { Index: 110 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago3d.toISOString(),
      Spec: {
        Name: "monitoring_prometheus",
        Labels: monitoringLabels("prom/prometheus:v3.2.1"),
        TaskTemplate: {
          ContainerSpec: {
            Image: `prom/prometheus:v3.2.1@${digestPrometheus}`,
            Args: [
              "--config.file=/etc/prometheus/prometheus.yml",
              "--storage.tsdb.path=/prometheus",
            ],
            Mounts: [
              { Type: "volume", Source: "monitoring_prometheus-data", Target: "/prometheus" },
            ],
            Configs: [
              {
                ConfigID: idCfgPromConfig,
                ConfigName: "monitoring_prometheus-config",
                File: {
                  Name: "/etc/prometheus/prometheus.yml",
                  UID: "0",
                  GID: "0",
                  Mode: 0o444,
                },
              },
            ],
          },
          Resources: {
            Limits: { NanoCPUs: 1_000_000_000, MemoryBytes: 1024 * 1024 * 1024 },
            Reservations: { NanoCPUs: 250_000_000, MemoryBytes: 256 * 1024 * 1024 },
          },
          Networks: [{ Target: idNetMonitoring }],
        },
        Mode: { Replicated: { Replicas: 1 } },
      },
    },

    // monitoring_grafana
    {
      ID: idSvcGrafana,
      Version: { Index: 111 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago3d.toISOString(),
      Spec: {
        Name: "monitoring_grafana",
        Labels: monitoringLabels("grafana/grafana:11.5.2"),
        TaskTemplate: {
          ContainerSpec: {
            Image: `grafana/grafana:11.5.2@${digestGrafana}`,
            Env: ["GF_SECURITY_ADMIN_PASSWORD=admin"],
          },
          Resources: {
            Limits: { NanoCPUs: 500_000_000, MemoryBytes: 256 * 1024 * 1024 },
            Reservations: { NanoCPUs: 100_000_000, MemoryBytes: 128 * 1024 * 1024 },
          },
          Networks: [{ Target: idNetMonitoring }],
        },
        Mode: { Replicated: { Replicas: 1 } },
      },
    },

    // monitoring_node-exporter (global)
    {
      ID: idSvcNodeExporter,
      Version: { Index: 112 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago4d.toISOString(),
      Spec: {
        Name: "monitoring_node-exporter",
        Labels: monitoringLabels("prom/node-exporter:v1.9.0"),
        TaskTemplate: {
          ContainerSpec: {
            Image: `prom/node-exporter:v1.9.0@${digestNodeExporter}`,
          },
          Resources: {
            Limits: { NanoCPUs: 200_000_000, MemoryBytes: 64 * 1024 * 1024 },
            Reservations: { NanoCPUs: 50_000_000, MemoryBytes: 32 * 1024 * 1024 },
          },
          Networks: [{ Target: idNetMonitoring }],
        },
        Mode: { Global: {} },
      },
    },

    // infra_proxy
    {
      ID: idSvcProxy,
      Version: { Index: 120 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago30m.toISOString(),
      Spec: {
        Name: "infra_proxy",
        Labels: infraLabels("traefik:v3.3"),
        TaskTemplate: {
          ContainerSpec: {
            Image: `traefik:v3.3@${digestTraefik}`,
            Configs: [
              {
                ConfigID: idCfgProxyConf,
                ConfigName: "infra_proxy-config",
                File: { Name: "/etc/traefik/traefik.toml", UID: "0", GID: "0", Mode: 0o444 },
              },
            ],
          },
          Resources: {
            Limits: { NanoCPUs: 500_000_000, MemoryBytes: 256 * 1024 * 1024 },
            Reservations: { NanoCPUs: 100_000_000, MemoryBytes: 64 * 1024 * 1024 },
          },
          Networks: [{ Target: idNetInfra }],
        },
        Mode: { Replicated: { Replicas: 2 } },
        EndpointSpec: {
          Ports: [
            { Protocol: "tcp", TargetPort: 80, PublishedPort: 80, PublishMode: "ingress" },
            { Protocol: "tcp", TargetPort: 443, PublishedPort: 443, PublishMode: "ingress" },
          ],
        },
      },
      Endpoint: {
        Ports: [
          { Protocol: "tcp", TargetPort: 80, PublishedPort: 80, PublishMode: "ingress" },
          { Protocol: "tcp", TargetPort: 443, PublishedPort: 443, PublishMode: "ingress" },
        ],
      },
      UpdateStatus: {
        State: "completed",
        StartedAt: ago1h.toISOString(),
        CompletedAt: ago30m.toISOString(),
        Message: "update completed",
      },
    },

    // infra_registry
    {
      ID: idSvcRegistry,
      Version: { Index: 121 },
      CreatedAt: ago4d.toISOString(),
      UpdatedAt: ago4d.toISOString(),
      Spec: {
        Name: "infra_registry",
        Labels: infraLabels("registry:2"),
        TaskTemplate: {
          ContainerSpec: {
            Image: `registry:2@${digestRegistry}`,
          },
          Resources: {
            Limits: { NanoCPUs: 500_000_000, MemoryBytes: 256 * 1024 * 1024 },
            Reservations: { NanoCPUs: 100_000_000, MemoryBytes: 64 * 1024 * 1024 },
          },
          Networks: [{ Target: idNetInfra }],
        },
        Mode: { Replicated: { Replicas: 1 } },
        EndpointSpec: {
          Ports: [
            { Protocol: "tcp", TargetPort: 5000, PublishedPort: 5000, PublishMode: "ingress" },
          ],
        },
      },
      Endpoint: {
        Ports: [{ Protocol: "tcp", TargetPort: 5000, PublishedPort: 5000, PublishMode: "ingress" }],
      },
    },
  ];
}

function buildTasks(services: Service[], nodesByID: Map<string, Node>): Task[] {
  let taskNum = 0;
  let containerNum = 0;

  const nextTaskID = (): string => {
    taskNum++;
    return `tk${String(taskNum).padStart(23, "0")}`;
  };

  const nextContainerID = (): string => {
    containerNum++;
    return String(containerNum).padStart(64, "0");
  };

  const workerNodes = [idNodeWorker1, idNodeWorker2];
  let workerIndex = 0;

  const nextWorker = (): string => {
    const node = workerNodes[workerIndex % workerNodes.length];
    workerIndex++;
    return node;
  };

  const serviceByIndex = (index: number): Service => services[index];

  const makeRunningTask = (
    serviceID: string,
    slot: number,
    nodeID: string,
    image: string,
    versionIndex: number,
  ): Task => {
    const id = nextTaskID();
    return {
      ID: id,
      Version: { Index: versionIndex },
      ServiceID: serviceID,
      NodeID: nodeID,
      Slot: slot,
      Status: {
        Timestamp: ago2h.toISOString(),
        State: "running",
        Message: "started",
        ContainerStatus: {
          ContainerID: nextContainerID(),
          ExitCode: 0,
        },
      },
      DesiredState: "running",
      Spec: {
        ContainerSpec: { Image: image },
      },
      ServiceName: services.find((s) => s.ID === serviceID)?.Spec.Name,
      NodeHostname: nodesByID.get(nodeID)?.Description.Hostname,
    };
  };

  const getImage = (index: number): string =>
    serviceByIndex(index).Spec.TaskTemplate.ContainerSpec?.Image ?? "";

  const tasks: Task[] = [];

  // webshop_frontend — 3 replicas
  for (let slot = 1; slot <= 3; slot++) {
    tasks.push(makeRunningTask(idSvcFrontend, slot, nextWorker(), getImage(0), 200));
  }

  // webshop_api — 3 replicas, slot 2 has a failed predecessor
  tasks.push(makeRunningTask(idSvcAPI, 1, nextWorker(), getImage(1), 201));

  // Slot 2: failed task (older)
  const failedTaskID = nextTaskID();
  nextContainerID(); // consume a container ID
  tasks.push({
    ID: failedTaskID,
    Version: { Index: 202 },
    ServiceID: idSvcAPI,
    NodeID: idNodeWorker1,
    Slot: 2,
    Status: {
      Timestamp: ago1h.toISOString(),
      State: "failed",
      Message: "started",
      Err: "task: non-zero exit (137): OOM killed",
      ContainerStatus: {
        ContainerID: String(containerNum).padStart(64, "0"),
        ExitCode: 137,
      },
    },
    DesiredState: "running",
    Spec: {
      ContainerSpec: { Image: getImage(1) },
    },
    ServiceName: "webshop_api",
    NodeHostname: nodesByID.get(idNodeWorker1)?.Description.Hostname,
  });

  // Slot 2: replacement running task (newer)
  tasks.push(makeRunningTask(idSvcAPI, 2, idNodeWorker2, getImage(1), 203));

  // Slot 3
  tasks.push(makeRunningTask(idSvcAPI, 3, nextWorker(), getImage(1), 204));

  // webshop_worker — 2 replicas
  for (let slot = 1; slot <= 2; slot++) {
    tasks.push(makeRunningTask(idSvcWorker, slot, nextWorker(), getImage(2), 210));
  }

  // webshop_db — 1 replica
  tasks.push(makeRunningTask(idSvcDB, 1, idNodeWorker1, getImage(3), 220));

  // webshop_cache — 1 replica
  tasks.push(makeRunningTask(idSvcCache, 1, idNodeWorker2, getImage(4), 221));

  // webshop_search — 2 replicas
  for (let slot = 1; slot <= 2; slot++) {
    tasks.push(makeRunningTask(idSvcSearch, slot, nextWorker(), getImage(5), 230));
  }

  // monitoring_prometheus — 1 replica (on manager)
  tasks.push(makeRunningTask(idSvcPrometheus, 1, idNodeManager1, getImage(6), 240));

  // monitoring_grafana — 1 replica
  tasks.push(makeRunningTask(idSvcGrafana, 1, idNodeWorker1, getImage(7), 241));

  // monitoring_node-exporter — global (1 per node, slot=0)
  const allNodes = [idNodeManager1, idNodeWorker1, idNodeWorker2];
  for (const nodeID of allNodes) {
    tasks.push(makeRunningTask(idSvcNodeExporter, 0, nodeID, getImage(8), 250));
  }

  // infra_proxy — 2 replicas + 2 old shutdown tasks
  tasks.push(makeRunningTask(idSvcProxy, 1, nextWorker(), getImage(9), 260));
  tasks.push(makeRunningTask(idSvcProxy, 2, nextWorker(), getImage(9), 261));

  // Old shutdown tasks from previous update
  for (let slot = 1; slot <= 2; slot++) {
    const shutdownID = nextTaskID();
    nextContainerID();
    tasks.push({
      ID: shutdownID,
      Version: { Index: 255 },
      ServiceID: idSvcProxy,
      NodeID: workerNodes[(slot - 1) % workerNodes.length],
      Slot: slot,
      Status: {
        Timestamp: ago1h.toISOString(),
        State: "shutdown",
        Message: "shutdown",
        ContainerStatus: {
          ContainerID: String(containerNum).padStart(64, "0"),
          ExitCode: 0,
        },
      },
      DesiredState: "shutdown",
      Spec: {
        ContainerSpec: { Image: getImage(9) },
      },
      ServiceName: "infra_proxy",
      NodeHostname: nodesByID.get(workerNodes[(slot - 1) % workerNodes.length])?.Description
        .Hostname,
    });
  }

  // infra_registry — 1 replica
  tasks.push(makeRunningTask(idSvcRegistry, 1, idNodeWorker1, getImage(10), 270));

  return tasks;
}

export function buildDataset(): Dataset {
  const nodes = buildNodes();
  const networks = buildNetworks();
  const volumes = buildVolumes();
  const swarm = buildSwarm();
  const configs = buildConfigs();
  const secrets = buildSecrets();
  const services = buildServices();

  const nodesByID = new Map(nodes.map((n) => [n.ID, n]));
  const servicesByID = new Map(services.map((s) => [s.ID, s]));
  const configsByID = new Map(configs.map((c) => [c.ID, c]));
  const secretsByID = new Map(secrets.map((s) => [s.ID, s]));
  const networksByID = new Map(networks.map((n) => [n.Id, n]));
  const volumesByName = new Map(volumes.map((v) => [v.Name, v]));

  const tasks = buildTasks(services, nodesByID);
  const tasksByID = new Map(tasks.map((t) => [t.ID, t]));

  return {
    nodes,
    services,
    tasks,
    configs,
    secrets,
    networks,
    volumes,
    swarm,
    nodesByID,
    servicesByID,
    tasksByID,
    configsByID,
    secretsByID,
    networksByID,
    volumesByName,
  };
}

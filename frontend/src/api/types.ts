export interface Node {
  ID: string;
  Version: { Index: number };
  Spec: {
    Role: "worker" | "manager";
    Availability: string;
    Labels: Record<string, string> | null;
  };
  Description: {
    Hostname: string;
    Platform: { Architecture: string; OS: string };
    Resources: { NanoCPUs: number; MemoryBytes: number };
    Engine: { EngineVersion: string };
  };
  Status: {
    State: string;
    Addr: string;
  };
  ManagerStatus?: {
    Leader: boolean;
    Reachability: string;
    Addr: string;
  };
}

export interface Service {
  ID: string;
  Version: { Index: number };
  CreatedAt?: string;
  UpdatedAt?: string;
  Spec: {
    Name: string;
    Labels: Record<string, string> | null;
    TaskTemplate: {
      ContainerSpec?: {
        Image: string;
        Command?: string[] | null;
        Args?: string[] | null;
        Env?: string[] | null;
        Dir?: string;
        User?: string;
        Hostname?: string;
        Init?: boolean;
        StopSignal?: string;
        StopGracePeriod?: number;
        ReadOnly?: boolean;
        TTY?: boolean;
        Groups?: string[] | null;
        Hosts?: string[] | null;
        DNSConfig?: {
          Nameservers?: string[] | null;
          Search?: string[] | null;
          Options?: string[] | null;
        };
        CapabilityAdd?: string[] | null;
        CapabilityDrop?: string[] | null;
        Healthcheck?: {
          Test?: string[] | null;
          Interval?: number;
          Timeout?: number;
          Retries?: number;
          StartPeriod?: number;
          StartInterval?: number;
        };
        Configs?: Array<{
          ConfigID: string;
          ConfigName: string;
          File?: { Name: string; UID: string; GID: string; Mode: number };
        }> | null;
        Secrets?: Array<{
          SecretID: string;
          SecretName: string;
          File?: { Name: string; UID: string; GID: string; Mode: number };
        }> | null;
        Mounts?: ServiceMount[] | null;
      } | null;
      Resources?: {
        Limits?: { NanoCPUs?: number; MemoryBytes?: number; Pids?: number };
        Reservations?: { NanoCPUs?: number; MemoryBytes?: number };
      };
      RestartPolicy?: {
        Condition?: string;
        Delay?: number;
        MaxAttempts?: number;
        Window?: number;
      };
      Placement?: {
        Constraints?: string[] | null;
        Preferences?: Array<{ Spread?: { SpreadDescriptor: string } }> | null;
        MaxReplicas?: number;
      };
      LogDriver?: { Name: string; Options?: Record<string, string> };
      Networks?: Array<{ Target: string; Aliases?: string[] | null }> | null;
    };
    Mode: {
      Replicated?: { Replicas?: number };
      Global?: Record<string, never>;
    };
    UpdateConfig?: {
      Parallelism: number;
      Delay?: number;
      FailureAction?: string;
      Monitor?: number;
      MaxFailureRatio?: number;
      Order?: string;
    };
    RollbackConfig?: {
      Parallelism: number;
      Delay?: number;
      FailureAction?: string;
      Monitor?: number;
      MaxFailureRatio?: number;
      Order?: string;
    };
    EndpointSpec?: {
      Mode?: string;
      Ports?: Array<{
        Protocol: string;
        TargetPort: number;
        PublishedPort: number;
        PublishMode: string;
      }> | null;
    };
  };
  Endpoint?: {
    Ports?: Array<{
      Protocol: string;
      TargetPort: number;
      PublishedPort: number;
      PublishMode: string;
    }> | null;
    VirtualIPs?: Array<{
      NetworkID: string;
      Addr: string;
    }> | null;
  };
  PreviousSpec?: Service["Spec"];
  UpdateStatus?: {
    State?: string;
    StartedAt?: string;
    CompletedAt?: string;
    Message?: string;
  };
}

export interface ServiceListItem extends Service {
  RunningTasks: number;
}

export interface Task {
  ID: string;
  Version: { Index: number };
  ServiceID: string;
  NodeID?: string;
  ServiceName?: string;
  NodeHostname?: string;
  Slot?: number;
  Status: {
    Timestamp: string;
    State: string;
    Message: string;
    Err?: string;
    ContainerStatus?: {
      ContainerID: string;
      ExitCode: number;
    };
  };
  DesiredState: string;
  Spec: {
    ContainerSpec?: {
      Image: string;
    } | null;
  };
}

export interface Config {
  ID: string;
  Version: { Index: number };
  CreatedAt: string;
  UpdatedAt: string;
  Spec: {
    Name: string;
    Labels: Record<string, string> | null;
    Data?: string;
  };
}

export interface Secret {
  ID: string;
  Version: { Index: number };
  CreatedAt: string;
  UpdatedAt: string;
  Spec: {
    Name: string;
    Labels: Record<string, string> | null;
  };
}

export interface Network {
  Id: string; // Docker SDK: network.Summary uses "Id" not "ID"
  Name: string;
  Created: string;
  Driver: string;
  Scope: string;
  EnableIPv6: boolean;
  Internal: boolean;
  Attachable: boolean;
  Ingress: boolean;
  IPAM: {
    Driver?: string;
    Config: Array<{ Subnet: string; Gateway: string; IPRange?: string }> | null;
  };
  Options: Record<string, string> | null;
  Labels: Record<string, string> | null;
}

export interface Volume {
  Name: string;
  Driver: string;
  Labels: Record<string, string> | null;
  Mountpoint: string;
  Scope: string;
  Options: Record<string, string> | null;
  CreatedAt?: string;
}

export interface Stack {
  name: string;
  services: string[];
  configs: string[];
  secrets: string[];
  networks: string[];
  volumes: string[];
}

export interface PagedResponse<T> {
  items: T[];
  total: number;
}

export interface CollectionResponse<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}

export interface HistoryEntry {
  id: number;
  timestamp: string;
  type: string;
  action: string;
  resourceId: string;
  name: string;
  summary?: string;
}

export interface StackDetail {
  name: string;
  services: Service[];
  configs: Config[];
  secrets: Secret[];
  networks: Network[];
  volumes: Volume[];
}

export interface NetworkTopology {
  nodes: TopoServiceNode[];
  edges: TopoEdge[];
  networks: TopoNetwork[];
}

export interface TopoServiceNode {
  id: string;
  name: string;
  stack?: string;
  replicas: number;
  image: string;
  ports?: string[];
  mode: string;
  updateStatus?: string;
  networkAliases?: Record<string, string[]>;
}

export interface TopoEdge {
  source: string;
  target: string;
  networks: string[];
}

export interface TopoNetwork {
  id: string;
  name: string;
  driver: string;
  scope: string;
  stack?: string;
}

export interface PlacementTopology {
  nodes: TopoClusterNode[];
}

export interface TopoClusterNode {
  id: string;
  hostname: string;
  role: string;
  state: string;
  availability: string;
  tasks: TopoTask[];
}

export interface TopoTask {
  id: string;
  serviceId: string;
  serviceName: string;
  state: string;
  slot: number;
  image: string;
}

export interface SpecChange {
  field: string;
  old?: string;
  new?: string;
}

export interface ServiceRef {
  id: string;
  name: string;
}

export interface TraefikTLSDomain {
  main: string;
  sans?: string[];
}

export interface TraefikRouter {
  name: string;
  rule?: string;
  entrypoints?: string[];
  tls?: {
    certResolver?: string;
    domains?: TraefikTLSDomain[];
    options?: string;
  };
  middlewares?: string[];
  service?: string;
  priority?: number;
}

export interface TraefikService {
  name: string;
  port?: number;
  scheme?: string;
}

export interface TraefikMiddleware {
  name: string;
  type: string;
  config?: Record<string, string>;
}

export interface TraefikIntegration {
  name: "traefik";
  enabled: boolean;
  routers?: TraefikRouter[];
  services?: TraefikService[];
  middlewares?: TraefikMiddleware[];
}

export interface ShepherdIntegration {
  name: "shepherd";
  enabled: boolean;
  authConfig?: string;
}

export interface CronjobIntegration {
  name: "swarm-cronjob";
  enabled: boolean;
  schedule?: string;
  skipRunning?: boolean;
  replicas?: number;
  registryAuth?: boolean;
  queryRegistry?: boolean;
}

export interface DiunIntegration {
  name: "diun";
  enabled: boolean;
  watchRepo?: boolean;
  notifyOn?: string;
  maxTags?: number;
  includeTags?: string;
  excludeTags?: string;
  sortTags?: string;
  regopt?: string;
  hubLink?: string;
  platform?: string;
  metadata?: Record<string, string>;
}

export type Integration =
  | TraefikIntegration
  | ShepherdIntegration
  | CronjobIntegration
  | DiunIntegration;

export interface ServiceDetail {
  service: Service;
  changes?: SpecChange[];
  integrations?: Integration[];
}

export interface ConfigDetail {
  config: Config;
  services: ServiceRef[] | null;
}

export interface SecretDetail {
  secret: Secret;
  services: ServiceRef[] | null;
}

export interface NetworkDetail {
  network: Network;
  services: ServiceRef[] | null;
}

export interface VolumeDetail {
  volume: Volume;
  services: ServiceRef[] | null;
}

export interface StackSummary {
  name: string;
  serviceCount: number;
  configCount: number;
  secretCount: number;
  networkCount: number;
  volumeCount: number;
  desiredTasks: number;
  tasksByState: Record<string, number>;
  updatingServices: number;
  memoryLimitBytes: number;
  cpuLimitCores: number;
  memoryUsageBytes: number;
  cpuUsagePercent: number;
}

// Global search
export type SearchResourceType =
  | "services"
  | "stacks"
  | "nodes"
  | "tasks"
  | "configs"
  | "secrets"
  | "networks"
  | "volumes";

export interface SearchResult {
  id: string;
  name: string;
  detail: string;
  state?: string;
}

export interface SearchResponse {
  query: string;
  results: Partial<Record<SearchResourceType, SearchResult[]>>;
  counts: Partial<Record<SearchResourceType, number>>;
  total: number;
}

export interface SwarmInfo {
  swarm: {
    ID: string;
    CreatedAt: string;
    UpdatedAt: string;
    Spec: {
      Annotations: { Name: string; Labels: Record<string, string> | null };
      Orchestration: { TaskHistoryRetentionLimit?: number };
      Raft: {
        SnapshotInterval: number;
        KeepOldSnapshots?: number;
        LogEntriesForSlowFollowers: number;
        ElectionTick: number;
        HeartbeatTick: number;
      };
      Dispatcher: { HeartbeatPeriod: number };
      CAConfig: {
        NodeCertExpiry: number;
        ExternalCAs?: Array<{
          Protocol: string;
          URL: string;
          Options?: Record<string, string>;
        }> | null;
        ForceRotate: number;
      };
      TaskDefaults: {
        LogDriver?: { Name: string; Options?: Record<string, string> };
      };
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
  };
  managerAddr: string;
}

export interface PluginPrivilege {
  Name: string;
  Description: string;
  Value: string[] | null;
}

export interface PluginMount {
  Name: string;
  Description: string;
  Settable: string[] | null;
  Source: string | null;
  Destination: string;
  Type: string;
  Options: string[] | null;
}

export interface PluginDevice {
  Name: string;
  Description: string;
  Settable: string[] | null;
  Path: string | null;
}

export interface PluginEnv {
  Name: string;
  Description: string;
  Settable: string[] | null;
  Value: string | null;
}

export interface Plugin {
  Id?: string;
  Name: string;
  Enabled: boolean;
  PluginReference?: string;
  Settings: {
    Mounts: PluginMount[] | null;
    Env: string[] | null;
    Args: string[] | null;
    Devices: PluginDevice[] | null;
  };
  Config: {
    DockerVersion?: string;
    Description: string;
    Documentation?: string;
    Entrypoint: string[] | null;
    WorkDir: string;
    User?: { UID: number; GID: number };
    Interface: {
      Types: string[] | null;
      Socket: string;
    };
    Network: { Type: string };
    Linux: {
      Capabilities: string[] | null;
      AllowAllDevices: boolean;
      Devices: PluginDevice[] | null;
    };
    Mounts: PluginMount[] | null;
    Env: PluginEnv[] | null;
    Args: {
      Name: string;
      Description: string;
      Settable: string[] | null;
      Value: string[] | null;
    };
  };
}

export interface DiskUsageSummary {
  type: "images" | "containers" | "volumes" | "buildCache";
  count: number;
  active: number;
  totalSize: number;
  reclaimable: number;
}

export interface TargetStatus {
  targets: number;
  nodes: number;
}

export interface MonitoringStatus {
  prometheusConfigured: boolean;
  prometheusReachable: boolean;
  error?: string;
  nodeExporter: TargetStatus | null;
  cadvisor: TargetStatus | null;
}

export interface Identity {
  subject: string;
  displayName: string;
  email?: string;
  groups?: string[];
  provider: string;
  raw?: Record<string, unknown>;
}

export interface ClusterCapacity {
  maxNodeCPU: number;
  maxNodeMemory: number;
  totalCPU: number;
  totalMemory: number;
  nodeCount: number;
}

export interface PatchOp {
  op: string;
  path: string;
  value?: string;
}

export interface PrometheusResponse {
  data: {
    resultType: "vector" | "matrix" | "scalar" | "string";
    result: Array<{
      metric: Record<string, string>;
      value?: [number, string];
      values?: [number, string][];
    }>;
  };
}

export type Healthcheck = NonNullable<
  NonNullable<Service["Spec"]["TaskTemplate"]["ContainerSpec"]>["Healthcheck"]
>;

export type Placement = NonNullable<Service["Spec"]["TaskTemplate"]["Placement"]>;
export type PortConfig = NonNullable<
  NonNullable<NonNullable<Service["Spec"]["EndpointSpec"]>["Ports"]>[number]
>;
export type UpdateConfig = NonNullable<Service["Spec"]["UpdateConfig"]>;
export type LogDriver = NonNullable<Service["Spec"]["TaskTemplate"]["LogDriver"]>;

export interface ContainerConfig {
  command?: string[];
  args?: string[];
  dir: string;
  user: string;
  hostname: string;
  init?: boolean;
  tty: boolean;
  readOnly: boolean;
  stopSignal: string;
  stopGracePeriod?: number;
  capabilityAdd?: string[];
  capabilityDrop?: string[];
  groups?: string[];
  hosts?: string[];
  dnsConfig?: {
    nameservers?: string[];
    search?: string[];
    options?: string[];
  };
}

export interface ServiceConfigRef {
  configID: string;
  configName: string;
  fileName: string;
}

export interface ServiceSecretRef {
  secretID: string;
  secretName: string;
  fileName: string;
}

export interface ServiceNetworkRef {
  target: string;
  aliases?: string[];
}

export interface ServiceMount {
  Type: string;
  Source: string;
  Target: string;
  ReadOnly?: boolean;
  BindOptions?: {
    Propagation?: string;
    NonRecursive?: boolean;
    CreateMountpoint?: boolean;
  };
  VolumeOptions?: {
    NoCopy?: boolean;
    Labels?: Record<string, string>;
    Subpath?: string;
  };
  TmpfsOptions?: {
    SizeBytes?: number;
    Mode?: number;
  };
  ImageOptions?: {
    Subpath?: string;
  };
  ClusterOptions?: Record<string, unknown>;
}

export type RecommendationCategory =
  | "over-provisioned"
  | "approaching-limit"
  | "at-limit"
  | "no-limits"
  | "no-reservations"
  | "no-healthcheck"
  | "no-restart-policy"
  | "flaky-service"
  | "node-disk-full"
  | "node-memory-pressure"
  | "single-replica"
  | "manager-has-workloads"
  | "uneven-distribution";

export type RecommendationSeverity = "info" | "warning" | "critical";
export type RecommendationScope = "service" | "node" | "cluster";

export interface Recommendation {
  category: RecommendationCategory;
  severity: RecommendationSeverity;
  scope: RecommendationScope;
  targetId: string;
  targetName: string;
  resource: string;
  message: string;
  current: number;
  configured: number;
  suggested?: number;
  fixAction?: string;
}

export interface RecommendationSummary {
  critical: number;
  warning: number;
  info: number;
}

export interface RecommendationsResponse {
  items: Recommendation[];
  total: number;
  summary: RecommendationSummary;
  computedAt: string;
}

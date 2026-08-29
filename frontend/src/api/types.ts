export interface Node {
  "@id"?: string;
  "@type"?: string;
  ID: string;
  Version: { Index: number };
  Spec: {
    Role?: "worker" | "manager" | undefined;
    Availability?: string | undefined;
    Labels: Record<string, string> | null;
  };
  Description: {
    Hostname?: string | undefined;
    Platform: { Architecture?: string; OS?: string };
    Resources: { NanoCPUs?: number; MemoryBytes?: number };
    Engine: { EngineVersion?: string };
  };
  Status: {
    State?: string | undefined;
    Message?: string | undefined;
    Addr?: string | undefined;
  };
  ManagerStatus?: {
    Leader?: boolean | undefined;
    Reachability?: string | undefined;
    Addr?: string | undefined;
  };
}

export interface Service {
  ID: string;
  Version: { Index: number };
  CreatedAt?: string | undefined;
  UpdatedAt?: string | undefined;
  Spec: {
    Name: string;
    Labels: Record<string, string> | null;
    TaskTemplate?: {
      ContainerSpec?: {
        Image?: string | undefined;
        Command?: string[] | null | undefined;
        Args?: string[] | null | undefined;
        Env?: string[] | null | undefined;
        Dir?: string | undefined;
        User?: string | undefined;
        Hostname?: string | undefined;
        Init?: boolean | undefined;
        StopSignal?: string | undefined;
        StopGracePeriod?: number | undefined;
        ReadOnly?: boolean | undefined;
        TTY?: boolean | undefined;
        Groups?: string[] | null | undefined;
        Hosts?: string[] | null | undefined;
        DNSConfig?: {
          Nameservers?: string[] | null | undefined;
          Search?: string[] | null | undefined;
          Options?: string[] | null | undefined;
        };
        CapabilityAdd?: string[] | null | undefined;
        CapabilityDrop?: string[] | null | undefined;
        Healthcheck?: {
          Test?: string[] | null | undefined;
          Interval?: number | undefined;
          Timeout?: number | undefined;
          Retries?: number | undefined;
          StartPeriod?: number | undefined;
          StartInterval?: number | undefined;
        };
        Configs?: Array<{
          ConfigID: string;
          ConfigName: string;
          File?: { Name: string; UID: string; GID: string; Mode: number } | undefined;
        }> | null;
        Secrets?: Array<{
          SecretID: string;
          SecretName: string;
          File?: { Name: string; UID: string; GID: string; Mode: number } | undefined;
        }> | null;
        Mounts?: ServiceMount[] | null | undefined;
      } | null;
      Resources?: {
        Limits?: { NanoCPUs?: number; MemoryBytes?: number; Pids?: number } | undefined;
        Reservations?: { NanoCPUs?: number; MemoryBytes?: number } | undefined;
      };
      RestartPolicy?: {
        Condition?: string | undefined;
        Delay?: number | undefined;
        MaxAttempts?: number | undefined;
        Window?: number | undefined;
      };
      Placement?: {
        Constraints?: string[] | null | undefined;
        Preferences?: Array<{ Spread?: { SpreadDescriptor: string } }> | null | undefined;
        MaxReplicas?: number | undefined;
      };
      LogDriver?: { Name?: string; Options?: Record<string, string> } | undefined;
      Networks?: Array<{ Target?: string; Aliases?: string[] | null }> | null | undefined;
    };
    Mode: {
      Replicated?: { Replicas?: number } | undefined;
      Global?: Record<string, never> | undefined;
    };
    UpdateConfig?: {
      Parallelism: number;
      Delay?: number | undefined;
      FailureAction?: string | undefined;
      Monitor?: number | undefined;
      MaxFailureRatio?: number | undefined;
      Order?: string | undefined;
    };
    RollbackConfig?: {
      Parallelism: number;
      Delay?: number | undefined;
      FailureAction?: string | undefined;
      Monitor?: number | undefined;
      MaxFailureRatio?: number | undefined;
      Order?: string | undefined;
    };
    EndpointSpec?: {
      Mode?: string | undefined;
      Ports?: Array<{
        Protocol?: string | undefined;
        TargetPort?: number | undefined;
        PublishedPort?: number | undefined;
        PublishMode?: string | undefined;
      }> | null;
    };
  };
  Endpoint?: {
    Ports?: Array<{
      Protocol?: string | undefined;
      TargetPort?: number | undefined;
      PublishedPort?: number | undefined;
      PublishMode?: string | undefined;
    }> | null;
    VirtualIPs?: Array<{
      NetworkID?: string | undefined;
      Addr?: string | undefined;
    }> | null;
  };
  PreviousSpec?: Service["Spec"] | undefined;
  UpdateStatus?: {
    State?: string | undefined;
    StartedAt?: string | undefined;
    CompletedAt?: string | undefined;
    Message?: string | undefined;
  };
}

export interface ServiceListItem extends Service {
  "@id"?: string;
  "@type"?: string;
  RunningTasks: number;
}

export interface Task {
  "@id"?: string;
  "@type"?: string;
  ID: string;
  Version: { Index: number };
  ServiceID: string;
  NodeID?: string | undefined;
  ServiceName?: string | undefined;
  NodeHostname?: string | undefined;
  Slot?: number | undefined;
  Status: {
    Timestamp: string;
    State: string;
    Message?: string | undefined;
    Err?: string | undefined;
    ContainerStatus?: {
      ContainerID: string;
      ExitCode: number;
    };
  };
  DesiredState: string;
  Spec: {
    ContainerSpec?: {
      Image?: string | undefined;
    } | null;
  };
}

export interface Config {
  "@id"?: string;
  "@type"?: string;
  ID: string;
  Version: { Index: number };
  CreatedAt: string;
  UpdatedAt: string;
  Spec: {
    Name: string;
    Labels: Record<string, string> | null;
    Data?: string | undefined;
  };
}

export interface Secret {
  "@id"?: string;
  "@type"?: string;
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
  "@id"?: string;
  "@type"?: string;
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
    Driver?: string | undefined;
    Config: Array<{ Subnet?: string; Gateway?: string; IPRange?: string }> | null;
  };
  Options: Record<string, string> | null;
  Labels: Record<string, string> | null;
}

export interface Volume {
  "@id"?: string;
  "@type"?: string;
  Name: string;
  Driver: string;
  Labels: Record<string, string> | null;
  Mountpoint: string;
  Scope: string;
  Options: Record<string, string> | null;
  CreatedAt?: string | undefined;
}

export interface Stack {
  "@id"?: string;
  "@type"?: string;
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
  "@id"?: string;
  "@type"?: string;
  id: number;
  timestamp: string;
  type: string;
  action: string;
  resourceId: string;
  name: string;
  summary?: string | undefined;
}

export interface StackDetail {
  name: string;
  services: Service[];
  configs: Config[];
  secrets: Secret[];
  networks: Network[];
  volumes: Volume[];
}

export interface SpecChange {
  field: string;
  old?: string | undefined;
  new?: string | undefined;
}

export interface ServiceRef {
  id: string;
  name: string;
}

export interface TraefikTLSDomain {
  main: string;
  sans?: string[] | undefined;
}

export interface TraefikRouter {
  name: string;
  rule?: string | undefined;
  entrypoints?: string[] | undefined;
  tls?: {
    certResolver?: string | undefined;
    domains?: TraefikTLSDomain[] | undefined;
    options?: string | undefined;
  };
  middlewares?: string[] | undefined;
  service?: string | undefined;
  priority?: number | undefined;
}

export interface TraefikService {
  name: string;
  port?: number | undefined;
  scheme?: string | undefined;
}

export interface TraefikMiddleware {
  name: string;
  type: string;
  config?: Record<string, string> | undefined;
}

export interface TraefikIntegration {
  name: "traefik";
  enabled: boolean;
  routers?: TraefikRouter[] | undefined;
  services?: TraefikService[] | undefined;
  middlewares?: TraefikMiddleware[] | undefined;
}

export interface ShepherdIntegration {
  name: "shepherd";
  enabled: boolean;
  authConfig?: string | undefined;
}

export interface CronjobIntegration {
  name: "swarm-cronjob";
  enabled: boolean;
  schedule?: string | undefined;
  skipRunning?: boolean | undefined;
  replicas?: number | undefined;
  registryAuth?: boolean | undefined;
  queryRegistry?: boolean | undefined;
}

export interface DiunIntegration {
  name: "diun";
  enabled: boolean;
  watchRepo?: boolean | undefined;
  notifyOn?: string | undefined;
  maxTags?: number | undefined;
  includeTags?: string | undefined;
  excludeTags?: string | undefined;
  sortTags?: string | undefined;
  regopt?: string | undefined;
  hubLink?: string | undefined;
  platform?: string | undefined;
  metadata?: Record<string, string> | undefined;
}

export type Integration =
  | TraefikIntegration
  | ShepherdIntegration
  | CronjobIntegration
  | DiunIntegration;

export interface ServiceDetail {
  service: Service;
  changes?: SpecChange[] | undefined;
  integrations?: Integration[] | undefined;
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
  "@id"?: string;
  "@type"?: string;
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
  state?: string | undefined;
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
      Name?: string | undefined;
      Labels: Record<string, string> | null;
      Orchestration: { TaskHistoryRetentionLimit?: number };
      Raft: {
        SnapshotInterval?: number | undefined;
        KeepOldSnapshots?: number | undefined;
        LogEntriesForSlowFollowers?: number | undefined;
        ElectionTick: number;
        HeartbeatTick: number;
      };
      Dispatcher: { HeartbeatPeriod?: number };
      CAConfig: {
        NodeCertExpiry?: number | undefined;
        ExternalCAs?: Array<{
          Protocol: string;
          URL: string;
          Options?: Record<string, string> | undefined;
        }> | null;
        ForceRotate?: number | undefined;
      };
      TaskDefaults: {
        LogDriver?: { Name?: string; Options?: Record<string, string> } | undefined;
      };
      EncryptionConfig: { AutoLockManagers: boolean };
    };
    TLSInfo: {
      TrustRoot?: string | undefined;
      CertIssuerSubject?: string | undefined;
      CertIssuerPublicKey?: string | undefined;
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
  "@id"?: string;
  "@type"?: string;
  Id?: string | undefined;
  Name: string;
  Enabled: boolean;
  PluginReference?: string | undefined;
  Settings: {
    Mounts: PluginMount[] | null;
    Env: string[] | null;
    Args: string[] | null;
    Devices: PluginDevice[] | null;
  };
  Config: {
    DockerVersion?: string | undefined;
    Description: string;
    Documentation?: string | undefined;
    Entrypoint: string[] | null;
    WorkDir: string;
    User?: { UID?: number; GID?: number } | undefined;
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
  "@id"?: string;
  "@type"?: string;
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
  error?: string | undefined;
  nodeExporter: TargetStatus | null;
  cadvisor: TargetStatus | null;
}

export interface Identity {
  subject: string;
  displayName: string;
  email?: string | undefined;
  groups?: string[] | undefined;
  provider: string;
  raw?: Record<string, unknown> | undefined;
  permissions?: Record<string, string[]> | undefined;
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
  value?: string | undefined;
}

/** One series of a Prometheus `vector` or `matrix` result. */
export interface PrometheusSeries {
  /** Absent on `scalar` and `string` results, which carry no labels. */
  metric?: Record<string, string> | undefined;
  /** Present on `vector` results (a single sample). */
  value?: [number, string] | undefined;
  /** Present on `matrix` results (a sample list). */
  values?: [number, string][] | undefined;
}

/**
 * The Prometheus HTTP API envelope, proxied verbatim through `/metrics`.
 *
 * `data` is absent when Prometheus answers with an error envelope
 * (`{"status":"error","errorType":...,"error":...}`).
 */
export interface PrometheusResponse {
  status?: "success" | "error" | undefined;
  errorType?: string | undefined;
  error?: string | undefined;
  data?: {
    resultType: "vector" | "matrix" | "scalar" | "string";
    result: PrometheusSeries[];
  };
}

type TaskTemplate = NonNullable<Service["Spec"]["TaskTemplate"]>;

export type Healthcheck = NonNullable<NonNullable<TaskTemplate["ContainerSpec"]>["Healthcheck"]>;

export type Placement = NonNullable<TaskTemplate["Placement"]>;
export type PortConfig = NonNullable<
  NonNullable<NonNullable<Service["Spec"]["EndpointSpec"]>["Ports"]>[number]
>;
export type UpdateConfig = NonNullable<Service["Spec"]["UpdateConfig"]>;
export type LogDriver = NonNullable<TaskTemplate["LogDriver"]>;

export interface ContainerConfig {
  command?: string[] | null | undefined;
  args?: string[] | null | undefined;
  dir: string;
  user: string;
  hostname: string;
  init?: boolean | null | undefined;
  tty: boolean;
  readOnly: boolean;
  stopSignal: string;
  stopGracePeriod?: number | null | undefined;
  capabilityAdd?: string[] | null | undefined;
  capabilityDrop?: string[] | null | undefined;
  groups?: string[] | null | undefined;
  hosts?: string[] | null | undefined;
  dnsConfig?:
    | {
        nameservers?: string[] | null | undefined;
        search?: string[] | null | undefined;
        options?: string[] | null | undefined;
      }
    | null
    | undefined;
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
  aliases?: string[] | undefined;
}

export interface ServiceMount {
  Type?: string | undefined;
  Source?: string | undefined;
  Target?: string | undefined;
  ReadOnly?: boolean | undefined;
  BindOptions?: {
    Propagation?: string | undefined;
    NonRecursive?: boolean | undefined;
    CreateMountpoint?: boolean | undefined;
  };
  VolumeOptions?: {
    NoCopy?: boolean | undefined;
    Labels?: Record<string, string> | undefined;
    Subpath?: string | undefined;
  };
  TmpfsOptions?: {
    SizeBytes?: number | undefined;
    Mode?: number | undefined;
  };
  ImageOptions?: {
    Subpath?: string | undefined;
  };
  ClusterOptions?: Record<string, unknown> | undefined;
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
  suggested?: number | undefined;
  fixAction?: string | undefined;
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

export interface JGFDocument {
  graphs: JGFGraph[];
}

export interface JGFGraph {
  id: string;
  type: string;
  label: string;
  directed: boolean;
  metadata: JGFMetadata;
  nodes: Record<string, JGFNode>;
  edges?: JGFEdge[] | undefined;
  hyperedges?: JGFHyperedge[] | undefined;
}

export type JGFMetadata = Record<string, unknown> & { "@context": string };

export interface JGFNode {
  label: string;
  metadata: JGFMetadata;
}

export interface JGFEdge {
  source: string;
  target: string;
  metadata: JGFMetadata;
}

export interface JGFHyperedge {
  nodes: string[];
  metadata: JGFMetadata;
}

export interface LicenseEntry {
  id?: string | undefined;
  name?: string | undefined;
  url?: string | undefined;
}

export interface LicenseComponent {
  name: string;
  version?: string | undefined;
  description?: string | undefined;
  ecosystem: string;
  licenses: LicenseEntry[];
  homepage?: string | undefined;
  repository?: string | undefined;
}

export interface LicensesResponse {
  generatedAt?: string | undefined;
  components: LicenseComponent[];
}

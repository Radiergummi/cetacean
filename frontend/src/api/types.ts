export interface Node {
  ID: string;
  Version: { Index: number };
  Spec: {
    Role: string;
    Availability: string;
    Labels: Record<string, string>;
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
    Labels: Record<string, string>;
    TaskTemplate: {
      ContainerSpec: {
        Image: string;
        Command?: string[];
        Args?: string[];
        Env?: string[];
        Dir?: string;
        User?: string;
        Hostname?: string;
        Init?: boolean;
        StopSignal?: string;
        StopGracePeriod?: number;
        ReadOnly?: boolean;
        Healthcheck?: {
          Test?: string[];
          Interval?: number;
          Timeout?: number;
          Retries?: number;
          StartPeriod?: number;
        };
        Configs?: Array<{
          ConfigID: string;
          ConfigName: string;
          File?: { Name: string; UID: string; GID: string; Mode: number };
        }>;
        Secrets?: Array<{
          SecretID: string;
          SecretName: string;
          File?: { Name: string; UID: string; GID: string; Mode: number };
        }>;
        Mounts?: Array<{
          Type: string;
          Source: string;
          Target: string;
          ReadOnly?: boolean;
        }>;
      };
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
        Constraints?: string[];
        Preferences?: Array<{ Spread?: { SpreadDescriptor: string } }>;
        MaxReplicas?: number;
      };
      LogDriver?: { Name: string; Options?: Record<string, string> };
    };
    Mode: {
      Replicated?: { Replicas: number };
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
      }>;
    };
  };
  Endpoint?: {
    Ports?: Array<{
      Protocol: string;
      TargetPort: number;
      PublishedPort: number;
      PublishMode: string;
    }>;
  };
  UpdateStatus?: {
    State: string;
    StartedAt: string;
    CompletedAt: string;
    Message: string;
  };
}

export interface ServiceListItem extends Service {
  RunningTasks: number;
}

export interface Task {
  ID: string;
  Version: { Index: number };
  ServiceID: string;
  NodeID: string;
  ServiceName?: string;
  NodeHostname?: string;
  Slot: number;
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
    ContainerSpec: {
      Image: string;
    };
  };
}

export interface Config {
  ID: string;
  Version: { Index: number };
  CreatedAt: string;
  UpdatedAt: string;
  Spec: {
    Name: string;
    Labels: Record<string, string>;
  };
}

export interface Secret {
  ID: string;
  Version: { Index: number };
  CreatedAt: string;
  UpdatedAt: string;
  Spec: {
    Name: string;
    Labels: Record<string, string>;
  };
}

export interface Network {
  Id: string;
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
    Config: Array<{ Subnet: string; Gateway: string; IPRange?: string }>;
  };
  Options: Record<string, string>;
  Labels: Record<string, string>;
}

export interface Volume {
  Name: string;
  Driver: string;
  Labels: Record<string, string>;
  Mountpoint: string;
  Scope: string;
  Options: Record<string, string>;
  CreatedAt: string;
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

export interface ServiceRef {
  id: string;
  name: string;
}

export interface ConfigDetail {
  config: Config;
  services: ServiceRef[];
}

export interface SecretDetail {
  secret: Secret;
  services: ServiceRef[];
}

export interface NetworkDetail {
  network: Network;
  services: ServiceRef[];
}

export interface VolumeDetail {
  volume: Volume;
  services: ServiceRef[];
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

export interface NotificationRuleStatus {
  id: string;
  name: string;
  enabled: boolean;
  lastFired?: string;
  fireCount: number;
}

// Global search
export type SearchResourceType =
  | "services" | "stacks" | "nodes" | "tasks"
  | "configs" | "secrets" | "networks" | "volumes";

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

export interface Node {
  ID: string
  Version: { Index: number }
  Spec: {
    Role: string
    Availability: string
    Labels: Record<string, string>
  }
  Description: {
    Hostname: string
    Platform: { Architecture: string; OS: string }
    Resources: { NanoCPUs: number; MemoryBytes: number }
    Engine: { EngineVersion: string }
  }
  Status: {
    State: string
    Addr: string
  }
  ManagerStatus?: {
    Leader: boolean
    Reachability: string
    Addr: string
  }
}

export interface Service {
  ID: string
  Version: { Index: number }
  Spec: {
    Name: string
    Labels: Record<string, string>
    TaskTemplate: {
      ContainerSpec: {
        Image: string
      }
    }
    Mode: {
      Replicated?: { Replicas: number }
      Global?: Record<string, never>
    }
  }
  UpdateStatus?: {
    State: string
    StartedAt: string
    CompletedAt: string
    Message: string
  }
}

export interface Task {
  ID: string
  Version: { Index: number }
  ServiceID: string
  NodeID: string
  Slot: number
  Status: {
    Timestamp: string
    State: string
    Message: string
    ContainerStatus?: {
      ContainerID: string
      ExitCode: number
    }
  }
  DesiredState: string
  Spec: {
    ContainerSpec: {
      Image: string
    }
  }
}

export interface Config {
  ID: string
  Version: { Index: number }
  CreatedAt: string
  UpdatedAt: string
  Spec: {
    Name: string
    Labels: Record<string, string>
  }
}

export interface Secret {
  ID: string
  Version: { Index: number }
  CreatedAt: string
  UpdatedAt: string
  Spec: {
    Name: string
    Labels: Record<string, string>
  }
}

export interface Network {
  Id: string
  Name: string
  Driver: string
  Scope: string
  IPAM: {
    Config: Array<{ Subnet: string; Gateway: string }>
  }
  Labels: Record<string, string>
}

export interface Volume {
  Name: string
  Driver: string
  Labels: Record<string, string>
  Mountpoint: string
  Scope: string
  CreatedAt: string
}

export interface Stack {
  name: string
  services: string[]
  configs: string[]
  secrets: string[]
  networks: string[]
  volumes: string[]
}

export interface ClusterOverview {
  nodeCount: number
  serviceCount: number
  taskCount: number
  stackCount: number
}

import { randomHex, type Dataset } from "./dataset";
import type { SSEClients } from "./sseHandlers";
import { broadcast } from "./sseHandlers";
import type { Service, Task } from "@/api/types";

function nextTaskID(): string {
  return randomHex(25);
}

function nextContainerID(): string {
  return randomHex(64);
}

function pickRandom<T>(array: T[]): T {
  return array[Math.floor(Math.random() * array.length)];
}

function randomBetween(min: number, max: number): number {
  return min + Math.random() * (max - min);
}

/**
 * Get replicated services (those with Replicated mode and >0 replicas).
 */
function getReplicatedServices(dataset: Dataset) {
  return dataset.services.filter(
    (service) => service.Spec.Mode.Replicated && (service.Spec.Mode.Replicated.Replicas ?? 0) > 0,
  );
}

/**
 * Get running tasks for a given service.
 */
function getRunningTasks(dataset: Dataset, serviceId: string): Task[] {
  return dataset.tasks.filter(
    (task) => task.ServiceID === serviceId && task.Status.State === "running",
  );
}

/**
 * Get worker nodes (non-manager, active).
 */
function getWorkerNodes(dataset: Dataset) {
  return dataset.nodes.filter(
    (node) => node.Spec.Role === "worker" && node.Spec.Availability === "active",
  );
}

function addTask(dataset: Dataset, task: Task) {
  dataset.tasks.push(task);
  dataset.tasksByID.set(task.ID, task);
}

function broadcastTask(clients: SSEClients, action: string, task: Task) {
  broadcast(clients, "task", "tasks", task.ID, {
    type: "task",
    action,
    id: task.ID,
    resource: task,
  });
}

function makeTask(dataset: Dataset, service: Service, slot: number): Task {
  const workers = getWorkerNodes(dataset);
  const node = workers.length > 0 ? pickRandom(workers) : dataset.nodes[0];
  return {
    ID: nextTaskID(),
    Version: { Index: Math.floor(Math.random() * 10000) },
    ServiceID: service.ID,
    NodeID: node.ID,
    Slot: slot,
    Status: {
      Timestamp: new Date().toISOString(),
      State: "running",
      Message: "started",
      ContainerStatus: { ContainerID: nextContainerID(), ExitCode: 0 },
    },
    DesiredState: "running",
    Spec: { ContainerSpec: { Image: service.Spec.TaskTemplate.ContainerSpec?.Image ?? "" } },
    ServiceName: service.Spec.Name,
    NodeHostname: node.Description.Hostname,
  };
}

function broadcastService(clients: SSEClients, action: string, service: { ID: string }) {
  broadcast(clients, "service", "services", service.ID, {
    type: "service",
    action,
    id: service.ID,
    resource: service,
  });
}

function broadcastNode(clients: SSEClients, action: string, node: { ID: string }) {
  broadcast(clients, "node", "nodes", node.ID, {
    type: "node",
    action,
    id: node.ID,
    resource: node,
  });
}

/**
 * Task restart: pick a running task, fail it, then create a replacement.
 */
function scenarioTaskRestart(dataset: Dataset, clients: SSEClients) {
  const services = getReplicatedServices(dataset);

  if (services.length === 0) {
    return;
  }

  const service = pickRandom(services);
  const runningTasks = getRunningTasks(dataset, service.ID);

  if (runningTasks.length === 0) {
    return;
  }

  const task = pickRandom(runningTasks);

  // Mark the task as failed
  task.Status.State = "failed";
  task.Status.Message = "task: non-zero exit (1)";
  task.Status.Err = "task: non-zero exit (1)";
  task.Status.Timestamp = new Date().toISOString();

  if (task.Status.ContainerStatus) {
    task.Status.ContainerStatus.ExitCode = 1;
  }

  task.DesiredState = "shutdown";
  broadcastTask(clients, "update", task);

  // After a delay, create a replacement
  setTimeout(() => {
    const replacement = makeTask(dataset, service, task.Slot ?? 0);
    addTask(dataset, replacement);
    broadcastTask(clients, "create", replacement);
  }, 500);
}

/**
 * Service scale: bump replica count +1, then scale back after a delay.
 */
function scenarioServiceScale(dataset: Dataset, clients: SSEClients) {
  const services = getReplicatedServices(dataset);

  if (services.length === 0) {
    return;
  }

  const service = pickRandom(services);
  const replicated = service.Spec.Mode.Replicated!;
  const originalReplicas = replicated.Replicas ?? 1;

  // Scale up
  replicated.Replicas = originalReplicas + 1;
  service.Version.Index++;
  broadcastService(clients, "update", service);

  // Create the new task
  const newTask = makeTask(dataset, service, originalReplicas + 1);
  addTask(dataset, newTask);
  broadcastTask(clients, "create", newTask);

  // Scale back after 10-20s
  const delay = randomBetween(10000, 20000);

  setTimeout(() => {
    replicated.Replicas = originalReplicas;
    service.Version.Index++;
    broadcastService(clients, "update", service);

    // Mark the extra task as shutdown
    newTask.Status.State = "shutdown";
    newTask.Status.Message = "shutdown";
    newTask.Status.Timestamp = new Date().toISOString();
    newTask.DesiredState = "shutdown";
    broadcastTask(clients, "update", newTask);
  }, delay);
}

/**
 * Rolling update: set UpdateStatus to "updating", then mark "completed".
 */
function scenarioRollingUpdate(dataset: Dataset, clients: SSEClients) {
  const services = getReplicatedServices(dataset);

  if (services.length === 0) {
    return;
  }

  const service = pickRandom(services);

  // Start the update
  service.UpdateStatus = {
    State: "updating",
    StartedAt: new Date().toISOString(),
    Message: "update in progress",
  };
  service.Version.Index++;
  broadcastService(clients, "update", service);

  // Complete after 3-8s
  const delay = randomBetween(3000, 8000);

  setTimeout(() => {
    service.UpdateStatus = {
      State: "completed",
      StartedAt: service.UpdateStatus!.StartedAt,
      CompletedAt: new Date().toISOString(),
      Message: "update completed",
    };
    service.Version.Index++;
    broadcastService(clients, "update", service);
  }, delay);
}

/**
 * Task fail: set a task to failed with a random error, recover after a delay.
 */
function scenarioTaskFail(dataset: Dataset, clients: SSEClients) {
  const services = getReplicatedServices(dataset);

  if (services.length === 0) {
    return;
  }

  const service = pickRandom(services);
  const runningTasks = getRunningTasks(dataset, service.ID);

  if (runningTasks.length === 0) {
    return;
  }

  const task = pickRandom(runningTasks);

  const errors = [
    "task: non-zero exit (137): OOM killed",
    "task: non-zero exit (1): application error",
    "task: non-zero exit (143): SIGTERM",
    "container unhealthy: health check failed",
  ];

  // Fail the task
  task.Status.State = "failed";
  task.Status.Err = pickRandom(errors);
  task.Status.Message = "started";
  task.Status.Timestamp = new Date().toISOString();

  if (task.Status.ContainerStatus) {
    task.Status.ContainerStatus.ExitCode = 137;
  }

  task.DesiredState = "shutdown";
  broadcastTask(clients, "update", task);

  // Recover: create a replacement
  setTimeout(
    () => {
      const replacement = makeTask(dataset, service, task.Slot ?? 0);
      addTask(dataset, replacement);
      broadcastTask(clients, "create", replacement);
    },
    randomBetween(3000, 8000),
  );
}

/**
 * Node pressure: set a worker node to "down", recover after a delay.
 */
function scenarioNodePressure(dataset: Dataset, clients: SSEClients) {
  const workers = getWorkerNodes(dataset);

  if (workers.length === 0) {
    return;
  }

  const node = pickRandom(workers);

  // Mark node as down
  node.Status.State = "down";
  broadcastNode(clients, "update", node);

  // Recover after 2-5s
  const delay = randomBetween(2000, 5000);

  setTimeout(() => {
    node.Status.State = "ready";
    broadcastNode(clients, "update", node);
  }, delay);
}

const scenarios = [
  { weight: 40, run: scenarioTaskRestart },
  { weight: 15, run: scenarioServiceScale },
  { weight: 20, run: scenarioRollingUpdate },
  { weight: 15, run: scenarioTaskFail },
  { weight: 10, run: scenarioNodePressure },
] as const;

const totalWeight = scenarios.reduce((sum, s) => sum + s.weight, 0);

function pickScenario(): (dataset: Dataset, clients: SSEClients) => void {
  let roll = Math.random() * totalWeight;

  for (const scenario of scenarios) {
    roll -= scenario.weight;

    if (roll <= 0) {
      return scenario.run;
    }
  }

  return scenarios[0].run;
}

/**
 * Start the event simulator. Runs scenarios at random intervals (5-15s).
 */
export function startSimulator(dataset: Dataset, clients: SSEClients): void {
  function scheduleNext() {
    const delay = 5000 + Math.random() * 10000;

    setTimeout(() => {
      const scenario = pickScenario();
      scenario(dataset, clients);
      scheduleNext();
    }, delay);
  }

  scheduleNext();
}

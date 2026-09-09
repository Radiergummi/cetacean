import { z } from "zod";

/**
 * Runtime shapes for the responses the dashboard decodes.
 *
 * `fetchJSON` used to cast `res.json()` straight to its type parameter, so a
 * server the dashboard disagreed with rendered as blank cells and `undefined`
 * with nothing anywhere saying why. These schemas are checked at the boundary
 * and **only warn**: the data is handed on either way, so behaviour against a
 * server that has drifted is exactly what it was, and the drift becomes
 * something a developer sees in the console and a test can assert on.
 *
 * Two rules keep this from becoming a second, rotting copy of `types.ts`:
 *
 * - Every object is *loose*. A field the server adds is not drift, and a schema
 *   that failed on one would report every deployment running ahead of its
 *   dashboard.
 * - Only fields the dashboard actually reads are declared. Mirroring all sixty
 *   fields of a `swarm.Service` would catch nothing the dashboard would have
 *   noticed and would need editing every time the Docker SDK moved.
 *
 * So a schema here answers one question: is this still the shape the pages were
 * written against?
 */

const version = z.looseObject({ Index: z.number() });

/** Labels come back as a JSON `null` rather than an empty object when unset. */
const labels = z.record(z.string(), z.string()).nullable();

export const nodeSchema = z.looseObject({
  ID: z.string(),
  Version: version,
  Spec: z.looseObject({ Labels: labels }),
  Description: z.looseObject({
    Platform: z.looseObject({}),
    Resources: z.looseObject({}),
    Engine: z.looseObject({}),
  }),
  Status: z.looseObject({ State: z.string() }),
});

export const serviceSchema = z.looseObject({
  ID: z.string(),
  Version: version,
  Spec: z.looseObject({ Name: z.string() }),
});

export const taskSchema = z.looseObject({
  ID: z.string(),
  Version: version,
  ServiceID: z.string(),
  Status: z.looseObject({ Timestamp: z.string(), State: z.string() }),
  DesiredState: z.string(),
});

export const configSchema = z.looseObject({
  ID: z.string(),
  Version: version,
  Spec: z.looseObject({ Name: z.string() }),
});

export const secretSchema = z.looseObject({
  ID: z.string(),
  Version: version,
  Spec: z.looseObject({ Name: z.string() }),
});

/** Docker's network summary keys the id as `Id`, not `ID`. */
export const networkSchema = z.looseObject({
  Id: z.string(),
  Name: z.string(),
  Driver: z.string(),
  Scope: z.string(),
});

export const volumeSchema = z.looseObject({
  Name: z.string(),
  Driver: z.string(),
  Mountpoint: z.string(),
  Scope: z.string(),
  Labels: labels,
  Options: labels,
});

export const pluginSchema = z.looseObject({
  Name: z.string(),
  Enabled: z.boolean(),
  Settings: z.looseObject({}),
  Config: z.looseObject({}),
});

const serviceRefs = z.array(z.looseObject({ id: z.string(), name: z.string() })).nullable();

export const serviceDetailSchema = z.looseObject({ service: serviceSchema });
export const configDetailSchema = z.looseObject({ config: configSchema, services: serviceRefs });
export const secretDetailSchema = z.looseObject({ secret: secretSchema, services: serviceRefs });
export const networkDetailSchema = z.looseObject({ network: networkSchema, services: serviceRefs });
export const volumeDetailSchema = z.looseObject({ volume: volumeSchema, services: serviceRefs });
export const nodeDetailSchema = z.looseObject({ node: nodeSchema });
export const taskDetailSchema = z.looseObject({ task: taskSchema });
export const pluginDetailSchema = z.looseObject({ plugin: pluginSchema });

export const stackDetailSchema = z.looseObject({
  name: z.string(),
  services: z.array(serviceSchema),
  configs: z.array(configSchema),
  secrets: z.array(secretSchema),
  networks: z.array(networkSchema),
  volumes: z.array(volumeSchema),
});

/** The list item, which is the summary a stack list shows, not the detail. */
export const stackListSchema = z.looseObject({
  name: z.string(),
  services: z.array(z.string()),
  configs: z.array(z.string()),
  secrets: z.array(z.string()),
  networks: z.array(z.string()),
  volumes: z.array(z.string()),
});

/** A service row carries the running count the list column shows. */
export const serviceListSchema = serviceSchema.extend({ RunningTasks: z.number() });

export const stackDetailResponseSchema = z.looseObject({ stack: stackDetailSchema });

export const stackSummarySchema = z.looseObject({
  name: z.string(),
  serviceCount: z.number(),
  desiredTasks: z.number(),
  tasksByState: z.record(z.string(), z.number()),
});

export const historyEntrySchema = z.looseObject({
  id: z.number(),
  timestamp: z.string(),
  type: z.string(),
  action: z.string(),
  resourceId: z.string(),
  name: z.string(),
});

export const diskUsageSchema = z.looseObject({
  type: z.string(),
  count: z.number(),
  totalSize: z.number(),
  reclaimable: z.number(),
});

/**
 * The list envelope. Pagination reads `total` against what it has loaded, so a
 * missing or renamed count is the one drift that visibly breaks a table — the
 * infinite scroll either stops early or never stops.
 */
function buildCollection<T extends z.ZodType>(item: T) {
  return z.looseObject({
    items: z.array(item),
    total: z.number(),
    limit: z.number(),
    offset: z.number(),
  });
}

const collections = new WeakMap<z.ZodType, unknown>();

/**
 * Memoised on the item schema, because the envelope depends on nothing else and
 * the callers build it per request: a paged list rebuilt the array schema, the
 * object schema and their parse machinery on every page and every refetch.
 */
export function collectionOf<T extends z.ZodType>(item: T): ReturnType<typeof buildCollection<T>> {
  const cached = collections.get(item);

  if (cached) {
    return cached as ReturnType<typeof buildCollection<T>>;
  }

  const collection = buildCollection(item);

  collections.set(item, collection);

  return collection;
}

/**
 * A JGF document, as `/topology` serves it.
 *
 * The graph is the one response that used to arrive unchecked, and it is also
 * the one whose drift has actually been felt: it carried no `runningReplicas`,
 * so every service on the topology view was painted as fully up. Node and edge
 * `metadata` stays open — what a vertex says about itself is per-type — but the
 * envelope and the JSON-LD context every projection promises are pinned.
 */
const jgfMetadataSchema = z.looseObject({ "@context": z.string() });

export const jgfDocumentSchema = z.looseObject({
  graphs: z.array(
    z.looseObject({
      id: z.string(),
      type: z.string(),
      label: z.string(),
      directed: z.boolean(),
      metadata: jgfMetadataSchema,
      nodes: z.record(
        z.string(),
        z.looseObject({ label: z.string(), metadata: jgfMetadataSchema }),
      ),
      edges: z
        .array(
          z.looseObject({
            source: z.string(),
            target: z.string(),
            metadata: jgfMetadataSchema,
          }),
        )
        .optional(),
      hyperedges: z
        .array(z.looseObject({ nodes: z.array(z.string()), metadata: jgfMetadataSchema }))
        .optional(),
    }),
  ),
});

export const searchSchema = z.looseObject({
  query: z.string(),
  results: z.record(
    z.string(),
    z.array(z.looseObject({ id: z.string(), name: z.string(), detail: z.string() })),
  ),
  counts: z.record(z.string(), z.number()),
  total: z.number(),
});

export const recommendationsSchema = z.looseObject({
  items: z.array(
    z.looseObject({
      category: z.string(),
      severity: z.string(),
      scope: z.string(),
      targetId: z.string(),
      targetName: z.string(),
      message: z.string(),
    }),
  ),
  total: z.number(),
  summary: z.looseObject({}),
  computedAt: z.string(),
});

export const clusterSchema = z.looseObject({
  nodeCount: z.number(),
  serviceCount: z.number(),
  taskCount: z.number(),
  stackCount: z.number(),
  tasksByState: z.record(z.string(), z.number()),
});

export const clusterCapacitySchema = z.looseObject({
  maxNodeCPU: z.number(),
  maxNodeMemory: z.number(),
  totalCPU: z.number(),
  totalMemory: z.number(),
  nodeCount: z.number(),
});

const usageSchema = z.looseObject({
  used: z.number(),
  total: z.number(),
  percent: z.number(),
});

export const clusterMetricsSchema = z.looseObject({
  cpu: usageSchema,
  memory: usageSchema,
  disk: usageSchema,
});

export const swarmSchema = z.looseObject({
  swarm: z.looseObject({
    ID: z.string(),
    Spec: z.looseObject({ Labels: labels }),
  }),
});

export const identitySchema = z.looseObject({
  subject: z.string(),
  displayName: z.string(),
  provider: z.string(),
});

export const monitoringStatusSchema = z.looseObject({
  prometheusConfigured: z.boolean(),
  prometheusReachable: z.boolean(),
  nodeExporter: z.looseObject({ targets: z.number(), nodes: z.number() }).nullable(),
  cadvisor: z.looseObject({ targets: z.number(), nodes: z.number() }).nullable(),
});

/**
 * Prometheus answers an error the same way it answers a hit — 200, with the
 * failure in the body — so `status` is the field that decides which, and the
 * result is optional because an error carries none.
 */
export const prometheusSchema = z.looseObject({
  status: z.string().optional(),
  data: z.looseObject({ resultType: z.string().optional() }).optional(),
});

export const logResponseSchema = z.looseObject({
  lines: z.array(z.looseObject({ timestamp: z.string(), message: z.string() })),
});

export const licensesSchema = z.looseObject({
  components: z.array(z.looseObject({ name: z.string() })),
});

export const healthSchema = z.looseObject({ status: z.string() });

export const containerConfigSchema = z.looseObject({
  containerConfig: z.looseObject({
    dir: z.string(),
    user: z.string(),
    hostname: z.string(),
    tty: z.boolean(),
    readOnly: z.boolean(),
  }),
});

const stringMap = z.record(z.string(), z.string());

export const envSchema = z.looseObject({ env: stringMap });
export const labelsSchema = z.looseObject({ labels: stringMap });

export const placementSchema = z.looseObject({ placement: z.looseObject({}) });
export const healthcheckSchema = z.looseObject({ healthcheck: z.looseObject({}).nullable() });
export const portsSchema = z.looseObject({ ports: z.array(z.looseObject({})) });
export const mountsSchema = z.looseObject({ mounts: z.array(z.looseObject({})) });
export const resourcesSchema = z.looseObject({ resources: z.record(z.string(), z.unknown()) });

export const serviceConfigsSchema = z.looseObject({
  configs: z.array(
    z.looseObject({ configID: z.string(), configName: z.string(), fileName: z.string() }),
  ),
});

export const serviceSecretsSchema = z.looseObject({
  secrets: z.array(
    z.looseObject({ secretID: z.string(), secretName: z.string(), fileName: z.string() }),
  ),
});

export const serviceNetworksSchema = z.looseObject({
  networks: z.array(z.looseObject({ target: z.string() })),
});

export const nodeRoleSchema = z.looseObject({
  role: z.string(),
  isLeader: z.boolean(),
  managerCount: z.number(),
});

export const unlockKeySchema = z.looseObject({ unlockKey: z.string() });
export const metricsLabelsSchema = z.looseObject({ data: z.array(z.string()) });
export const dockerVersionSchema = z.looseObject({ version: z.string(), url: z.string() });

import { RoutedEdge } from "./RoutedEdge";
import { EntrypointNode, MiddlewareNode, RouterNode, ServiceNode } from "./TraefikGraphNodes";
import type { TraefikIntegration } from "@/api/types";
import { traefikIntegrationToReactFlow } from "@/lib/traefikGraph";
import { layoutTraefikGraph } from "@/lib/traefikLayout";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  useEdgesState,
  useNodesInitialized,
  useNodesState,
  useReactFlow,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { useEffect, useMemo, useState } from "react";

const fitViewOptions = { padding: 0.15 };
const proOptions = { hideAttribution: true };
const edgeTypes = { traefikRouted: RoutedEdge };

const nodeTypes = {
  traefikEntrypoint: EntrypointNode,
  traefikRouter: RouterNode,
  traefikMiddleware: MiddlewareNode,
  traefikService: ServiceNode,
};

/**
 * Nodes are rendered unplaced so React Flow can measure them, and ELK is given
 * those measurements. Laying out from guessed sizes is what left edges hanging
 * in the empty half of an oversized box.
 */
function Canvas({ integration }: { integration: TraefikIntegration }) {
  const graph = useMemo(() => traefikIntegrationToReactFlow(integration), [integration]);
  const [nodes, setNodes, onNodesChange] = useNodesState(graph.nodes);
  const [edges, setEdges] = useEdgesState(graph.edges);
  const [placed, setPlaced] = useState(false);
  const measured = useNodesInitialized();
  const { fitView, getNodes } = useReactFlow();

  useEffect(() => {
    if (!measured || placed) {
      return;
    }

    let live = true;

    void layoutTraefikGraph(getNodes(), graph.edges).then((result) => {
      if (!live) {
        return;
      }

      setNodes(result.nodes);
      setEdges(result.edges);
      setPlaced(true);
    });

    return () => {
      live = false;
    };
  }, [measured, placed, graph, getNodes, setNodes, setEdges]);

  // Fitting inside the promise above measured the positions React had not
  // committed yet, which framed whichever node still sat at the origin.
  useEffect(() => {
    if (placed) {
      void fitView(fitViewOptions);
    }
  }, [placed, fitView]);

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      onNodesChange={onNodesChange}
      nodeTypes={nodeTypes}
      edgeTypes={edgeTypes}
      proOptions={proOptions}
      nodesDraggable={false}
      nodesConnectable={false}
      className="transition-opacity duration-200"
      style={{ opacity: placed ? 1 : 0 }}
    >
      <Background />
    </ReactFlow>
  );
}

export default function TraefikGraph({ integration }: { integration: TraefikIntegration }) {
  // Remounting on a label change is what re-runs the layout: the measurement
  // plumbing seeds itself once and has no way to take a second graph.
  return (
    <ReactFlowProvider key={JSON.stringify(integration)}>
      <Canvas integration={integration} />
    </ReactFlowProvider>
  );
}

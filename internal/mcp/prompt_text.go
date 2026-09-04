package mcp

// Prompt message text. This is the closest thing Cetacean has to telling a
// model how to reason about a Swarm cluster, so it lives here — reviewable in
// one place, beside mcpInstructions in review terms — rather than inline in the
// catalog.
//
// Four rules, all enforced by tests in prompts_test.go:
//
//  1. Name the driven tools explicitly, in order. The sequence is the content;
//     a prompt that gestures at "investigate the service" adds nothing to the
//     tool list the model already has.
//  2. State the stopping condition — what to report instead of continuing.
//  3. A remediation prompt must not presume its own diagnosis. Each opens by
//     confirming the problem is real, and says to stop and report if it is not.
//  4. Make no claims about *this* cluster. The text is static and cannot know
//     the cluster's shape. Describe method; never assert that the cluster runs
//     three managers or uses overlay networks.
//
// No backticks: these are raw string literals, which are backtick-delimited.
// Tool names appear bare. No literal % either — the single %s is the
// interpolated argument.
const (
	promptTextDiagnoseService = `A service in this Swarm cluster is reported unhealthy: %s.

Work through this order and stop as soon as you can name the cause.

1. Resolve the name with the search tool to get the service ID, then read
   cetacean://services/<id> for its mode, desired replicas and update status.
2. Call list_resources for tasks and find this service's tasks. Compare the
   running count against desired. Note any task in failed, rejected or
   orphaned, and its Status.Err.
3. For the most recent failing task, call get_logs on the service. A crash loop
   usually names itself in the last lines before exit.
4. If the tasks are running but the service still misbehaves, call get_metrics
   for cpu and memory over the last hour. A container at its memory limit is
   killed and restarted without ever logging why.
5. Read cetacean://history for changes to this service. A fault that started
   minutes after an image update is usually the update.
6. Call get_recommendations and report any finding naming this service.

Report the cause, the evidence you used, and what you would change. Do not
apply a change as part of this investigation.`

	promptTextExplainUnschedulable = `Service %s has tasks Swarm has not placed. Four independent causes produce
this one symptom; check them in this order, because the cheap checks rule out
the expensive ones.

1. Resolve the name with the search tool, then read cetacean://services/<id>
   and note Spec.TaskTemplate.Placement - constraints, preferences and
   MaxReplicas.
2. Call list_resources for tasks, filter to this service, and read the pending
   task's Status.Err and Status.Message. Swarm often states the reason outright
   ("no suitable node"); if it does, stop there.
3. Call list_resources for nodes. For each placement constraint, check which
   nodes satisfy it - node.labels.* come from a node's Spec.Labels, node.role
   and node.hostname from its description. A constraint naming a label no node
   carries can never be satisfied.
4. Check each node's Availability. A drain node accepts nothing and a pause
   node accepts no new tasks, so a cluster can look healthy and still have
   nowhere to put this service.
5. Compare the task template's resource reservations against what nodes have
   left. Reservations, not limits, decide placement.

Report which of the four it is, and the specific constraint, label or
reservation responsible. If several apply, say so - fixing one leaves the task
pending.`

	promptTextReviewCapacity = `Review whether this Swarm cluster has capacity headroom.

1. Read cetacean://cluster for the node count and aggregate capacity.
2. Call list_resources for nodes and note each node's role, availability and
   resources. Manager nodes running workloads are a resilience risk even when
   they have room.
3. Call get_metrics for each node's cpu and memory over the last day, or the
   last week if load is weekly. Compare actual use against reservations rather
   than limits: Swarm schedules on reservations, so a cluster can refuse to
   place a task while sitting mostly idle.
4. Call get_recommendations and report every sizing and cluster finding.
5. State the headroom as "the largest service that could still be placed", not
   as a percentage - a percentage hides fragmentation across nodes.

Report where the cluster is constrained, which nodes are the constraint, and
whether the limit is reservations or real use. Do not change any reservation as
part of this review.`
)

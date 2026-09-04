package mcp

// Prompt message text. This is the closest thing Cetacean has to telling a
// model how to reason about a Swarm cluster, so it lives here — reviewable in
// one place, beside mcpInstructions in review terms — rather than inline in the
// catalog.
//
// Five rules shape this text, not all enforced the same way. 1 and 4 are
// tested across all six prompts (TestDiagnoseServiceExpandsItsArgument's
// driven-tool loop, generalized to the whole catalog, and
// TestPromptTextMakesNoClusterClaims). 2 and 3 apply only to the three
// remediation prompts and are both checked by
// TestRemediationPromptsConfirmBeforeActing. 5 is not tested at all — it
// holds structurally, because promptResult always builds exactly one user
// message and nothing in this package can construct a GetPromptResult any
// other way:
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
//  5. One user message. The prompt seeds a conversation; it never fabricates
//     an assistant turn.
//
// No backticks: these are raw string literals, which are backtick-delimited.
// Tool names appear bare. No literal % either — the single %s is the
// interpolated argument.
const (
	promptTextDiagnoseService = `A service in this Swarm cluster is reported unhealthy: %s.

Work through this order and stop as soon as you can name the cause.

1. Resolve the name with the find tool to get the service ID, then read
   cetacean://services/<id> for its mode, desired replicas and update status.
2. Call find for tasks and locate this service's tasks. Compare the
   running count against desired. Note any task in failed, rejected or
   orphaned, and its Status.Err. The call returns at most 200 items but
   reports the true total, so page with offset if you need more.
3. For the most recent failing task, call get_logs on the service. A crash loop
   usually names itself in the last lines before exit.
4. If the tasks are running but the service still misbehaves, call get_metrics
   for cpu and memory over the last hour. A container at its memory limit is
   killed and restarted without ever logging why.
5. Read cetacean://history for changes to this service. A fault that started
   minutes after an image update is usually the update. The history read
   returns the most recent entries across all resources, so if you do not see
   this service, say so rather than concluding nothing changed.
6. Call get_recommendations and report any finding naming this service.

Report the cause, the evidence you used, and what you would change. Do not
apply a change as part of this investigation.`

	promptTextExplainUnschedulable = `Service %s has tasks Swarm has not placed. Several independent causes produce
this one symptom; check them in this order, because the cheap checks rule out
the expensive ones.

1. Resolve the name with the find tool, then read cetacean://services/<id>
   and note Spec.TaskTemplate.Placement - constraints, preferences and
   MaxReplicas.
2. Call find for tasks, filter to this service, and read the pending
   task's Status.Err and Status.Message. Swarm often states the reason outright
   ("no suitable node"); if it does, stop there.
3. Call find for nodes. For each placement constraint, check which
   nodes satisfy it - node.labels.* come from a node's Spec.Labels, node.role
   from Spec.Role, and node.hostname and node.platform.* from its description;
   engine.labels.* come from Description.Engine.Labels. A constraint naming a
   label no node carries can never be satisfied.
4. Check each node's Availability and Status.State. A drain node accepts
   nothing and a pause node accepts no new tasks, and a node whose
   Status.State is down cannot take work either - so a cluster can look
   healthy and still have nowhere to put this service.
5. Compare the task template's resource reservations against what nodes have
   left. Reservations, not limits, decide placement. If Placement.MaxReplicas
   is set, check whether that per-node cap is already reached on every
   eligible node.

Report which cause it is, and the specific constraint, label or
reservation responsible. If several apply, say so - fixing one leaves the task
pending.`

	promptTextReviewCapacity = `Review whether this Swarm cluster has capacity headroom.

1. Read cetacean://cluster for the node count and aggregate capacity.
2. Call find for nodes and note each node's role, availability and
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

	promptTextRollBackService = `You have been asked to roll %s back to its previous version. Confirm the
rollback is warranted before performing it.

1. Resolve the name with the find tool, then read cetacean://services/<id>.
   Check UpdateStatus.State: rollback_started means a rollback is already
   running, so report that and stop rather than starting a second one;
   rollback_paused means one stalled and needs a human, not another attempt;
   updating means the deployment may still converge on its own.
2. Call find for tasks and confirm this service actually has failing
   or unplaced tasks. If every task is running and healthy, stop and report
   that - there is nothing to roll back, and rolling back a healthy service is
   an outage you caused.
3. Read cetacean://history to confirm what changed and when. A rollback undoes
   the last spec update, which may not be the change that broke it. The
   history read returns the most recent entries across all resources, so if
   you do not see this service, say so rather than concluding nothing changed.
4. Call rollback_service as a task (params.task, with a ttl), so the call
   reports when the previous version's replicas are actually running rather
   than when Docker accepted the request.
5. Poll tasks/get until the task settles. On failure read statusMessage: a
   rollback can fail for the same reason the update did.

Report the version rolled back to and the running replica count afterwards. If
step 2 showed a healthy service, report that and make no change.`

	promptTextRightSizeService = `Right-size the resource reservations for %s, using measured use rather than a
guess.

1. Call get_recommendations and find the sizing finding for this service.
   Cetacean computes it from observed use over the configured lookback window.
   If there is no finding, either the service is within thresholds or
   Cetacean cannot measure it - sizing findings require Prometheus. Confirm
   with get_metrics: if it reports metrics unavailable, say so and stop
   rather than reporting the service as correctly sized.
2. Resolve the name with the find tool, then read cetacean://services/<id>
   for its current Resources.Reservations and Resources.Limits.
3. Call get_metrics for cpu and memory over the last week and check the finding
   against the series yourself. A service whose load is weekly or bursty can
   look over-provisioned over a day and be correctly sized over a week.
4. Apply the change with update_service_resources. It sets the whole resource
   block in one call, so give it reservations and limits together, and never
   set a reservation above a limit.
5. Confirm the tasks rescheduled and are running. Changing reservations
   replaces every task, so this is a rolling restart, not a metadata edit.

Report the old and new values and the evidence for them. If the metric window
disagrees with the recommendation, report the disagreement and change nothing.`

	promptTextDrainNode = `Drain node %s for maintenance, without moving work somewhere it cannot run.

1. Resolve the name with the find tool, then read cetacean://nodes/<id> for
   its role, current availability and resources.
2. If this node is a manager, count the cluster's managers first: call
   find for nodes and count those whose Spec.Role is manager.
   Draining a manager does not remove it from the raft quorum, but losing it
   while drained does - and a two-manager quorum has no tolerance at all.
   Report and stop if the cluster has fewer than three managers, unless you
   have been told otherwise.
3. Call find for tasks and list every task currently on this node,
   with its service. This is the work that has to land elsewhere. The call
   returns at most 200 items but reports the true total, so page with offset
   until you have them all. If the list comes back empty, do not conclude the
   node is idle: confirm you can read tasks at all, and if you cannot, report
   that and leave the node active.
4. Call find for nodes and check that the remaining active nodes
   satisfy each of those services' placement constraints and have the
   reservations spare. A service constrained to this node alone has nowhere to
   go and will sit pending.
5. Only then call update_node_availability with drain.
6. Poll the affected services' tasks until they are running elsewhere. Report
   any that stayed pending, with the constraint that blocked them.

Report what moved, where it moved to, and anything that could not move. If step
2 or 4 found a blocker, report it and leave the node active.`
)

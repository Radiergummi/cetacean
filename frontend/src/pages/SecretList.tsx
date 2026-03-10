import {useCallback} from "react";
import {useNavigate} from "react-router-dom";
import {api} from "../api/client";
import type {Secret} from "../api/types";
import type {Column} from "../components/DataTable";
import DataTable from "../components/DataTable";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import {SkeletonTable} from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import ResourceCard from "../components/ResourceCard";
import ResourceName from "../components/ResourceName";
import SearchInput from "../components/SearchInput";
import SortIndicator from "../components/SortIndicator";
import TimeAgo from "../components/TimeAgo";
import ViewToggle from "../components/ViewToggle";
import {useSearchParam} from "../hooks/useSearchParam";
import {useSortParams} from "../hooks/useSort";
import {useSwarmResource} from "../hooks/useSwarmResource";
import {useViewMode} from "../hooks/useViewMode";

export default function SecretList() {
    const navigate = useNavigate();
    const [search, debouncedSearch, setSearch] = useSearchParam("q");
    const {sortKey, sortDir, toggle} = useSortParams("name");
    const {
        data: secrets,
        loading,
        error,
        retry,
    } = useSwarmResource(
        useCallback(
            () => api.secrets({search: debouncedSearch, sort: sortKey, dir: sortDir}),
            [debouncedSearch, sortKey, sortDir],
        ),
        "secret",
        (s: Secret) => s.ID,
    );

    const columns: Column<Secret>[] = [
        {
            header: <SortIndicator label="Name" active={sortKey === "name"} dir={sortDir}/>,
            cell: ({ID, Spec: {Name}}) => <ResourceName name={Name || ID}/>,
            onHeaderClick: () => toggle("name"),
        },
        {
            header: <SortIndicator label="Created" active={sortKey === "created"} dir={sortDir}/>,
            cell: ({CreatedAt}) => (
                CreatedAt ? <TimeAgo date={CreatedAt}/> : "\u2014"
            ),
            onHeaderClick: () => toggle("created"),
        },
        {
            header: <SortIndicator label="Updated" active={sortKey === "updated"} dir={sortDir}/>,
            cell: ({UpdatedAt}) => (
                UpdatedAt ? <TimeAgo date={UpdatedAt}/> : "\u2014"
            ),
            onHeaderClick: () => toggle("updated"),
        },
    ];
    const [viewMode, setViewMode] = useViewMode("secrets");

    if (loading) {
        return (
            <div>
                <PageHeader title="Secrets"/>
                <SkeletonTable columns={3}/>
            </div>
        );
    }
    if (error) {
        return <FetchError message={error.message} onRetry={retry}/>;
    }

    return (
        <div>
            <PageHeader title="Secrets"/>
            <p className="text-sm text-muted-foreground mb-4">
                Metadata only. Secret values are never exposed.
            </p>
            <div className="flex items-stretch gap-3 mb-4">
                <SearchInput value={search} onChange={setSearch} placeholder="Search secrets…"/>
                <ViewToggle mode={viewMode} onChange={setViewMode}/>
            </div>
            {secrets.length === 0 ? (
                <EmptyState message={search ? "No secrets match your search" : "No secrets found"}/>
            ) : viewMode === "table" ? (
                <DataTable
                    columns={columns}
                    data={secrets}
                    keyFn={({ID}) => ID}
                    onRowClick={({ID}) => navigate(`/secrets/${ID}`)}
                />
            ) : (
                <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                    {secrets.map((secret) => (
                        <ResourceCard
                            key={secret.ID}
                            title={<ResourceName name={secret.Spec.Name || secret.ID} />}
                            to={`/secrets/${secret.ID}`}
                        >
                            <div className="space-y-1 text-xs text-muted-foreground">
                                <div>Created: {secret.CreatedAt ? <TimeAgo date={secret.CreatedAt}/> : "\u2014"}</div>
                                <div>Updated: {secret.UpdatedAt ? <TimeAgo date={secret.UpdatedAt}/> : "\u2014"}</div>
                            </div>
                        </ResourceCard>
                    ))}
                </div>
            )}
        </div>
    );
}

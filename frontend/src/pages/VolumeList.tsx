import {useCallback} from "react";
import {useNavigate} from "react-router-dom";
import {api} from "../api/client";
import type {Volume} from "../api/types";
import type {Column} from "../components/DataTable";
import DataTable from "../components/DataTable";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import {SkeletonTable} from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import ResourceCard from "../components/ResourceCard";
import ResourceName from "../components/ResourceName";
import {SearchInput} from "../components/search";
import SortIndicator from "../components/SortIndicator";
import ViewToggle from "../components/ViewToggle";
import {useSearchParam} from "../hooks/useSearchParam";
import {useSortParams} from "../hooks/useSort";
import {useSwarmResource} from "../hooks/useSwarmResource";
import {useViewMode} from "../hooks/useViewMode";

export default function VolumeList() {
    const navigate = useNavigate();
    const [search, debouncedSearch, setSearch] = useSearchParam("q");
    const {sortKey, sortDir, toggle} = useSortParams("name");
    const {
        data: volumes,
        loading,
        error,
        retry,
    } = useSwarmResource(
        useCallback(
            () => api.volumes({search: debouncedSearch, sort: sortKey, dir: sortDir}),
            [debouncedSearch, sortKey, sortDir],
        ),
        "volume",
        (v: Volume) => v.Name,
    );

    const columns: Column<Volume>[] = [
        {
            header: <SortIndicator label="Name" active={sortKey === "name"} dir={sortDir}/>,
            cell: (v) => <ResourceName name={v.Name}/>,
            onHeaderClick: () => toggle("name"),
        },
        {
            header: <SortIndicator label="Driver" active={sortKey === "driver"} dir={sortDir}/>,
            cell: (v) => v.Driver,
            onHeaderClick: () => toggle("driver"),
        },
        {
            header: <SortIndicator label="Scope" active={sortKey === "scope"} dir={sortDir}/>,
            cell: (v) => v.Scope,
            onHeaderClick: () => toggle("scope"),
        },
    ];
    const [viewMode, setViewMode] = useViewMode("volumes");

    if (loading) {
        return (
            <div>
                <PageHeader title="Volumes"/>
                <SkeletonTable columns={3}/>
            </div>
        );
    }
    if (error) {
        return <FetchError message={error.message} onRetry={retry}/>;
    }

    return (
        <div>
            <PageHeader title="Volumes"/>
            <div className="flex items-stretch gap-3 mb-4">
                <SearchInput value={search} onChange={setSearch} placeholder="Search volumes..."/>
                <ViewToggle mode={viewMode} onChange={setViewMode}/>
            </div>
            {volumes.length === 0 ? (
                <EmptyState message={search ? "No volumes match your search" : "No volumes found"}/>
            ) : viewMode === "table" ? (
                <DataTable
                    columns={columns}
                    data={volumes}
                    keyFn={(v) => v.Name}
                    onRowClick={(v) => navigate(`/volumes/${v.Name}`)}
                />
            ) : (
                <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                    {volumes.map(({Driver, Name, Scope}) => (
                        <ResourceCard
                            key={Name}
                            title={<ResourceName name={Name}/>}
                            to={`/volumes/${Name}`}
                            meta={[Driver, Scope]}
                        />
                    ))}
                </div>
            )}
        </div>
    );
}

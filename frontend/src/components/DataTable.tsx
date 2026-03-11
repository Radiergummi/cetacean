import {useVirtualizer} from "@tanstack/react-virtual";
import {type ReactNode, type RefObject, useRef} from "react";

interface Column<T> {
    header: ReactNode;
    cell: (item: T) => ReactNode;
    className?: string;
    onHeaderClick?: () => void;
}

interface Props<T> {
    columns: Column<T>[];
    data: T[];
    keyFn: (item: T) => string;
    rowClassName?: (item: T) => string;
    onRowClick?: (item: T) => void;
}

const VIRTUAL_THRESHOLD = 100;
const ROW_HEIGHT_ESTIMATE = 48;

function PlainBody<T>({columns, data, keyFn, rowClassName, onRowClick}: Props<T>) {
    return (
        <tbody>
        {data.map((item) => (
            <tr
                key={keyFn(item)}
                data-clickable={onRowClick ? "" : undefined}
                className={`border-b last:border-b-0 data-clickable:cursor-pointer data-clickable:hover:bg-muted/50 ${rowClassName?.(
                    item) ?? ""}`}
                onClick={onRowClick ? () => onRowClick(item) : undefined}
            >
                {columns.map((column, index) => (
                    <td key={index} className={`p-3 text-sm ${column.className ?? ""}`}>
                        {column.cell(item)}
                    </td>
                ))}
            </tr>
        ))}
        </tbody>
    );
}

function VirtualBody<T>({
    columns,
    data,
    keyFn,
    rowClassName,
    onRowClick,
    scrollRef,
}: Props<T> & { scrollRef: RefObject<HTMLDivElement | null> }) {
    const virtualizer = useVirtualizer({
        count: data.length,
        getScrollElement: () => scrollRef.current,
        estimateSize: () => ROW_HEIGHT_ESTIMATE,
        overscan: 20,
    });

    const virtualItems = virtualizer.getVirtualItems();
    const totalSize = virtualizer.getTotalSize();

    return (
        <tbody>
        {virtualItems.length > 0 && (
            <tr>
                <td style={{height: virtualItems[0].start, padding: 0}} colSpan={columns.length}/>
            </tr>
        )}
        {virtualItems.map((virtualRow) => {
            const item = data[virtualRow.index];
            return (
                <tr
                    key={keyFn(item)}
                    ref={virtualizer.measureElement}
                    data-index={virtualRow.index}
                    data-clickable={onRowClick ? "" : undefined}
                    data-last={virtualRow.index === data.length - 1 || undefined}
                    className={`border-b data-last:border-b-0 data-clickable:cursor-pointer data-clickable:hover:bg-muted/50 ${rowClassName?.(item) ?? ""}`}
                    onClick={onRowClick ? () => onRowClick(item) : undefined}
                >
                    {columns.map((column, index) => (
                        <td key={index} className={`p-3 text-sm ${column.className ?? ""}`}>
                            {column.cell(item)}
                        </td>
                    ))}
                </tr>
            );
        })}
        {virtualItems.length > 0 && (
            <tr>
                <td
                    style={{height: totalSize - virtualItems[virtualItems.length - 1].end, padding: 0}}
                    colSpan={columns.length}
                />
            </tr>
        )}
        </tbody>
    );
}

export default function DataTable<T>({columns, data, keyFn, rowClassName, onRowClick}: Props<T>) {
    const scrollRef = useRef<HTMLDivElement>(null);
    const useVirtual = data.length > VIRTUAL_THRESHOLD;

    return (
        <div
            ref={scrollRef}
            data-virtual={useVirtual || undefined}
            className="overflow-x-auto rounded-lg border data-virtual:max-h-[calc(100vh-16rem)] data-virtual:overflow-y-auto"
        >
            <table className="w-full">
                <thead className="sticky top-0 z-10 bg-background">
                <tr className="border-b bg-muted/50">
                    {columns.map((column, index) => (
                        <th
                            key={index}
                            data-clickable={column.onHeaderClick ? "" : undefined}
                            className={`text-left p-3 text-sm font-medium ${column.className ??
                            ""} data-clickable:cursor-pointer data-clickable:select-none data-clickable:hover:bg-muted/80`}
                            onClick={column.onHeaderClick}
                        >
                            {column.header}
                        </th>
                    ))}
                </tr>
                </thead>
                {useVirtual ? (
                    <VirtualBody
                        columns={columns}
                        data={data}
                        keyFn={keyFn}
                        rowClassName={rowClassName}
                        onRowClick={onRowClick}
                        scrollRef={scrollRef}
                    />
                ) : (
                    <PlainBody
                        columns={columns}
                        data={data}
                        keyFn={keyFn}
                        rowClassName={rowClassName}
                        onRowClick={onRowClick}
                    />
                )}
            </table>
        </div>
    );
}

export type {Column};

import { cn } from "@/lib/utils";
import { Menu as MenuPrimitive } from "@base-ui/react/menu";

const itemClass =
  "flex w-full cursor-default items-center gap-3 rounded-sm px-2 py-1.5 text-sm outline-none " +
  "data-highlighted:bg-accent data-highlighted:text-accent-foreground " +
  "data-disabled:pointer-events-none data-disabled:text-muted-foreground";

/**
 * A menu's popup, portalled and placed below its trigger's end.
 */
function MenuContent({ className, ...props }: MenuPrimitive.Popup.Props) {
  return (
    <MenuPrimitive.Portal>
      <MenuPrimitive.Positioner
        align="end"
        sideOffset={4}
        className="z-50"
      >
        <MenuPrimitive.Popup
          data-slot="menu-content"
          className={cn(
            "min-w-36 rounded-md border bg-popover p-1 text-popover-foreground shadow-md outline-hidden",
            className,
          )}
          {...props}
        />
      </MenuPrimitive.Positioner>
    </MenuPrimitive.Portal>
  );
}

function MenuItem({ className, ...props }: MenuPrimitive.Item.Props) {
  return (
    <MenuPrimitive.Item
      data-slot="menu-item"
      className={cn(itemClass, className)}
      {...props}
    />
  );
}

function MenuRadioItem({ className, ...props }: MenuPrimitive.RadioItem.Props) {
  return (
    <MenuPrimitive.RadioItem
      data-slot="menu-radio-item"
      className={cn(
        itemClass,
        "justify-between data-checked:bg-accent data-checked:text-accent-foreground",
        className,
      )}
      {...props}
    />
  );
}

export { MenuContent, MenuItem, MenuRadioItem };

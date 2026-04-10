import { api } from "../../api/client";
import type { SwarmInfo } from "../../api/types";
import { Spinner } from "../Spinner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "../ui/alert-dialog";
import { Button } from "../ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "../ui/dialog";
import { Input } from "../ui/input";
import { Switch } from "../ui/switch";
import { KVTable } from "../data";
import { EditablePanel } from "../service-detail/EditablePanel";
import { useAsyncAction } from "../../hooks/useAsyncAction";
import { Check, Copy, KeyRound, LockOpen, RefreshCw } from "lucide-react";
import { useState } from "react";

type SwarmSpec = SwarmInfo["swarm"]["Spec"];

interface EncryptionPanelProps {
  spec: SwarmSpec;
  canEdit: boolean;
  onSaved: () => void;
}

export function EncryptionPanel({ spec, canEdit, onSaved }: EncryptionPanelProps) {
  const [draftAutoLock, setDraftAutoLock] = useState(false);

  const [unlockKeyValue, setUnlockKeyValue] = useState<string | null>(null);
  const [showUnlockKey, setShowUnlockKey] = useState(false);
  const [unlockKeyCopied, setUnlockKeyCopied] = useState(false);
  const fetchUnlockKey = useAsyncAction({ toast: true });
  const rotateUnlockKey = useAsyncAction({ toast: true });

  const [unlockInput, setUnlockInput] = useState("");
  const [unlockOpen, setUnlockOpen] = useState(false);
  const unlockSwarm = useAsyncAction({ toast: true });

  return (
    <div className="space-y-3">
      <EditablePanel
        title="Encryption"
        canEdit={canEdit}
        display={
          <KVTable
            rows={[
              ["Auto-Lock Managers", spec.EncryptionConfig.AutoLockManagers ? "Yes" : "No"],
            ]}
          />
        }
        edit={
          <label className="flex items-center gap-3 text-sm">
            <Switch
              checked={draftAutoLock}
              onCheckedChange={setDraftAutoLock}
            />
            Auto-Lock Managers
          </label>
        }
        onOpen={() => {
          setDraftAutoLock(spec.EncryptionConfig.AutoLockManagers);
        }}
        onSave={async () => {
          await api.patchSwarmEncryption({ AutoLockManagers: draftAutoLock });
          onSaved();
        }}
      />

      {spec.EncryptionConfig.AutoLockManagers && (
        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={fetchUnlockKey.loading}
            onClick={() => {
              if (showUnlockKey) {
                setShowUnlockKey(false);
                setUnlockKeyValue(null);
              } else {
                void fetchUnlockKey.execute(async () => {
                  const result = await api.unlockKey();
                  setUnlockKeyValue(result.unlockKey);
                  setShowUnlockKey(true);
                }, "Failed to fetch unlock key");
              }
            }}
          >
            {fetchUnlockKey.loading ? (
              <Spinner className="size-3" />
            ) : (
              <KeyRound className="size-3.5" />
            )}
            {showUnlockKey ? "Hide Unlock Key" : "Show Unlock Key"}
          </Button>

          <AlertDialog>
            <AlertDialogTrigger
              render={
                <Button
                  variant="outline"
                  size="sm"
                  disabled={rotateUnlockKey.loading}
                >
                  {rotateUnlockKey.loading ? (
                    <Spinner className="size-3" />
                  ) : (
                    <RefreshCw className="size-3.5" />
                  )}
                  Rotate Unlock Key
                </Button>
              }
            />
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Rotate unlock key?</AlertDialogTitle>
                <AlertDialogDescription>
                  This will invalidate the current unlock key. Make sure to save the new key.
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Cancel</AlertDialogCancel>
                <AlertDialogAction
                  onClick={() =>
                    void rotateUnlockKey.execute(async () => {
                      await api.rotateUnlockKey();
                      setShowUnlockKey(false);
                      setUnlockKeyValue(null);
                      onSaved();
                    }, "Failed to rotate unlock key")
                  }
                >
                  Rotate
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>

          <Dialog
            open={unlockOpen}
            onOpenChange={(open) => {
              setUnlockOpen(open);

              if (!open) {
                setUnlockInput("");
              }
            }}
          >
            <DialogTrigger
              render={
                <Button
                  variant="outline"
                  size="sm"
                  disabled={unlockSwarm.loading}
                >
                  {unlockSwarm.loading ? (
                    <Spinner className="size-3" />
                  ) : (
                    <LockOpen className="size-3.5" />
                  )}
                  Unlock Swarm
                </Button>
              }
            />
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Unlock swarm</DialogTitle>
                <DialogDescription>
                  Enter the unlock key to unlock a locked swarm manager.
                </DialogDescription>
              </DialogHeader>
              <Input
                placeholder="SWMKEY-1-..."
                value={unlockInput}
                onChange={(event) => setUnlockInput(event.target.value)}
                className="font-mono"
              />
              <DialogFooter>
                <Button
                  disabled={unlockSwarm.loading || !unlockInput.trim()}
                  onClick={() =>
                    void unlockSwarm.execute(async () => {
                      await api.unlockSwarm(unlockInput.trim());
                      setUnlockOpen(false);
                      setUnlockInput("");
                      onSaved();
                    }, "Failed to unlock swarm")
                  }
                >
                  {unlockSwarm.loading ? <Spinner className="size-3" /> : null}
                  Unlock
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </div>
      )}

      {showUnlockKey && unlockKeyValue && (
        <div className="space-y-2">
          <pre className="rounded-lg bg-muted/50 p-3 font-mono text-xs select-all">
            {unlockKeyValue}
          </pre>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              navigator.clipboard.writeText(unlockKeyValue).then(() => {
                setUnlockKeyCopied(true);
                setTimeout(() => setUnlockKeyCopied(false), 2000);
              });
            }}
          >
            {unlockKeyCopied ? (
              <>
                <Check className="size-3" />
                Copied
              </>
            ) : (
              <>
                <Copy className="size-3" />
                Copy
              </>
            )}
          </Button>
        </div>
      )}
    </div>
  );
}

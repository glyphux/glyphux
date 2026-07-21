import { useEditor } from "@craftjs/core";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { FieldControl } from "@/pages/content/fields";
import { useBlockRegistry } from "./registry-context";

/** The props editor for whichever BlockNode is currently selected in the
 * canvas — driven by that block's own Definition.Props (a
 * map[string]contract.Field, surfaced over the wire as sdk-js's
 * BlockDefinition.props), rendered one FieldControl per prop exactly the
 * way pages/content/fields.tsx already does for Layer-1 content-type
 * fields (same per-field-kind switch, reused rather than reinvented). */
export function PropsEditor() {
  const registry = useBlockRegistry();
  const { selectedId, blockType, blockProps, isRoot } = useEditor((state, query) => {
    const events = state.events.selected;
    const id = events.size > 0 ? [...events][0] : undefined;
    const node = id ? state.nodes[id] : undefined;
    return {
      selectedId: id,
      blockType: node?.data.props.blockType as string | undefined,
      blockProps: (node?.data.props.blockProps as Record<string, unknown> | undefined) ?? {},
      isRoot: id ? query.node(id).isRoot() : false,
    };
  });
  const { actions } = useEditor();

  if (!selectedId || isRoot || !blockType) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Properties</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-muted-foreground text-body">Select a block to edit its properties.</p>
        </CardContent>
      </Card>
    );
  }

  const def = registry.find((d) => d.name === blockType);

  return (
    <Card>
      <CardHeader>
        <CardTitle>{def?.display_name ?? blockType}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {!def && <p className="text-destructive text-body">This block type is no longer registered on the server.</p>}
        {def && Object.keys(def.props ?? {}).length === 0 && (
          <p className="text-muted-foreground text-body">This block has no editable properties.</p>
        )}
        {def &&
          Object.entries(def.props ?? {}).map(([name, field]) => {
            const fieldId = `block-prop-${name}`;
            return (
              <div key={name} className="flex flex-col gap-1.5">
                <Label htmlFor={fieldId}>
                  {name}
                  {field.required && <span className="text-destructive"> *</span>}
                </Label>
                <FieldControl
                  id={fieldId}
                  field={field}
                  value={blockProps[name]}
                  onChange={(value) =>
                    actions.setProp(selectedId, (props: Record<string, unknown>) => {
                      props.blockProps = { ...(props.blockProps as Record<string, unknown> | undefined), [name]: value };
                    })
                  }
                />
              </div>
            );
          })}
        <Button
          variant="destructive"
          size="sm"
          onClick={() => {
            actions.delete(selectedId);
          }}
        >
          Remove block
        </Button>
      </CardContent>
    </Card>
  );
}

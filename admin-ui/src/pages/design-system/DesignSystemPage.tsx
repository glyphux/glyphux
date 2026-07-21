import { useState } from "react";
import { FileText, Image as ImageIcon } from "lucide-react";
import { useToast } from "@/lib/toast-context";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Pagination } from "@/components/ui/pagination";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { EmptyState } from "@/components/layout/EmptyState";
import { ErrorState } from "@/components/layout/ErrorState";
import { ListSkeleton } from "@/components/layout/ListSkeleton";

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-4">
      <div>
        <h2 className="text-h1 font-semibold">{title}</h2>
        <Separator className="mt-2" />
      </div>
      <div className="flex flex-col gap-6">{children}</div>
    </section>
  );
}

function Swatch({ name, className }: { name: string; className: string }) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className={`h-14 w-full rounded-md border ${className}`} />
      <span className="text-muted-foreground text-small font-mono">{name}</span>
    </div>
  );
}

/** A component-preview surface for `admin-ui`'s design system (ticket DS
 * item 4). We chose an internal admin route over Storybook: this is a
 * single internal tool consumed by one team, not a component library
 * published/versioned for outside consumers, so a route that reuses the
 * app's own providers/router/theming and needs no separate build
 * toolchain, iframe sandboxing, or `.stories.tsx` convention is a better
 * cost/benefit fit — see docs/implementation/active/0020-design-system.md
 * for the fuller rationale. Every component this ticket touches (and the
 * pre-existing library from slice 1.13) gets at least one real, working
 * instance here so a new page author can see it before rebuilding it. */
export function DesignSystemPage() {
  const { toast } = useToast();
  const [radioValue, setRadioValue] = useState("draft");
  const [page, setPage] = useState(1);

  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-12 pb-16">
      <div>
        <h1 className="text-display font-semibold">Design System</h1>
        <p className="text-muted-foreground text-body mt-1">
          Every reusable component in <code className="text-mono">admin-ui</code>, with its variants, so new pages
          reuse rather than reinvent. Source: <code className="text-mono">src/components/ui/</code> and{" "}
          <code className="text-mono">src/components/layout/</code>.
        </p>
      </div>

      <Section title="Colors">
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <Swatch name="background" className="bg-background" />
          <Swatch name="card" className="bg-card" />
          <Swatch name="primary" className="bg-primary" />
          <Swatch name="secondary" className="bg-secondary" />
          <Swatch name="muted" className="bg-muted" />
          <Swatch name="accent" className="bg-accent" />
          <Swatch name="destructive" className="bg-destructive" />
          <Swatch name="success" className="bg-success" />
          <Swatch name="warning" className="bg-warning" />
          <Swatch name="border" className="bg-border" />
        </div>
      </Section>

      <Section title="Typography">
        <div className="flex flex-col gap-3">
          <p className="text-display">Display — text-display</p>
          <p className="text-h1">Heading 1 — text-h1</p>
          <p className="text-h2">Heading 2 — text-h2</p>
          <p className="text-body">Body — text-body, the default reading size for admin content.</p>
          <p className="text-small text-muted-foreground">Small — text-small, captions and metadata.</p>
          <p className="text-mono">Mono — text-mono, IDs and code.</p>
        </div>
      </Section>

      <Section title="Buttons">
        <div className="flex flex-wrap items-center gap-2">
          <Button>Primary</Button>
          <Button variant="secondary">Secondary</Button>
          <Button variant="destructive">Destructive</Button>
          <Button variant="outline">Outline</Button>
          <Button variant="ghost">Ghost</Button>
          <Button variant="link">Link</Button>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm">Small</Button>
          <Button size="default">Default</Button>
          <Button size="lg">Large</Button>
          <Button size="icon" aria-label="Icon button">
            <FileText />
          </Button>
          <Button disabled>Disabled</Button>
        </div>
      </Section>

      <Section title="Form controls">
        <div className="grid gap-6 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="ds-input">Text input</Label>
            <Input id="ds-input" placeholder="you@example.com" />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="ds-input-invalid">Text input (invalid)</Label>
            <Input id="ds-input-invalid" aria-invalid="true" defaultValue="not-an-email" />
            <p className="text-destructive text-small">Enter a valid email address.</p>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="ds-textarea">Textarea</Label>
            <Textarea id="ds-textarea" placeholder="Longer-form text…" rows={3} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="ds-select">Select</Label>
            <Select defaultValue="editor">
              <SelectTrigger id="ds-select">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="admin">Admin</SelectItem>
                <SelectItem value="editor">Editor</SelectItem>
                <SelectItem value="viewer">Viewer</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-2">
            <Label>Checkbox</Label>
            <div className="flex items-center gap-2">
              <Checkbox id="ds-checkbox" defaultChecked />
              <Label htmlFor="ds-checkbox">Notify me by email</Label>
            </div>
          </div>
          <div className="flex flex-col gap-2">
            <Label>Switch</Label>
            <div className="flex items-center gap-2">
              <Switch id="ds-switch" defaultChecked />
              <Label htmlFor="ds-switch">Enabled</Label>
            </div>
          </div>
          <div className="flex flex-col gap-2 sm:col-span-2">
            <Label>Radio group</Label>
            <RadioGroup value={radioValue} onValueChange={setRadioValue}>
              <div className="flex items-center gap-2">
                <RadioGroupItem value="draft" id="ds-radio-draft" />
                <Label htmlFor="ds-radio-draft">Draft</Label>
              </div>
              <div className="flex items-center gap-2">
                <RadioGroupItem value="published" id="ds-radio-published" />
                <Label htmlFor="ds-radio-published">Published</Label>
              </div>
            </RadioGroup>
          </div>
        </div>
      </Section>

      <Section title="Feedback">
        <div className="flex flex-col gap-3">
          <Alert>
            <AlertTitle>Heads up</AlertTitle>
            <AlertDescription>A default, informational alert.</AlertDescription>
          </Alert>
          <Alert variant="warning">
            <AlertTitle>Warning</AlertTitle>
            <AlertDescription>Something needs attention, but nothing broke.</AlertDescription>
          </Alert>
          <Alert variant="destructive">
            <AlertTitle>Error</AlertTitle>
            <AlertDescription>Something went wrong.</AlertDescription>
          </Alert>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Badge>Default</Badge>
          <Badge variant="secondary">Secondary</Badge>
          <Badge variant="outline">Outline</Badge>
          <Badge variant="success">Success</Badge>
          <Badge variant="warning">Warning</Badge>
          <Badge variant="destructive">Destructive</Badge>
        </div>
        <div className="flex flex-wrap items-center gap-4">
          <Spinner />
          <Button
            variant="outline"
            onClick={() => toast({ title: "This is a toast", description: "It auto-dismisses in 5s.", variant: "success" })}
          >
            Show toast
          </Button>
        </div>
        <div className="flex flex-col gap-2">
          <Label>Loading skeletons</Label>
          <ListSkeleton rows={3} />
          <div className="flex gap-2">
            <Skeleton className="size-10 rounded-full" />
            <Skeleton className="h-10 flex-1" />
          </div>
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <EmptyState icon={<ImageIcon />} title="No media yet" description="Upload your first asset." />
          <ErrorState error={new Error("The server could not be reached.")} onRetry={() => {}} />
        </div>
      </Section>

      <Section title="Data display">
        <Card>
          <CardHeader>
            <CardTitle>Card title</CardTitle>
            <CardDescription>A card groups related content with a consistent border/shadow.</CardDescription>
          </CardHeader>
          <CardContent>
            <p className="text-body">Card body content goes here.</p>
          </CardContent>
          <CardFooter>
            <Button size="sm">Action</Button>
          </CardFooter>
        </Card>

        <div className="flex items-center gap-3">
          <Avatar>
            <AvatarFallback>GX</AvatarFallback>
          </Avatar>
          <span className="text-body">Avatar with fallback initials</span>
        </div>

        <Tabs defaultValue="one">
          <TabsList>
            <TabsTrigger value="one">Tab one</TabsTrigger>
            <TabsTrigger value="two">Tab two</TabsTrigger>
          </TabsList>
          <TabsContent value="one">Content for tab one.</TabsContent>
          <TabsContent value="two">Content for tab two.</TabsContent>
        </Tabs>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Status</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow>
              <TableCell>Example row</TableCell>
              <TableCell>
                <Badge variant="success">Published</Badge>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>

        <Pagination page={page} totalPages={4} onPageChange={setPage} />
      </Section>
    </div>
  );
}

import { Link } from "react-router-dom";
import { FileText, Image, Shapes, Users } from "lucide-react";
import { useAuth } from "@/lib/auth-context";
import { allows } from "@/lib/permissions";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";

const TILES = [
  { to: "/content-types", icon: Shapes, title: "Content types", description: "Define the shape of your content." },
  { to: "/content", icon: FileText, title: "Content", description: "Create, edit, and publish content." },
  { to: "/media", icon: Image, title: "Media", description: "Upload and browse your media library." },
  { to: "/users", icon: Users, title: "Users", description: "Manage admin, editor, and viewer accounts.", capability: "users:manage" as const },
];

export function DashboardPage() {
  const { user } = useAuth();

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-display">Welcome back</h1>
        <p className="text-muted-foreground text-body mt-1">
          Signed in as {user?.email} ({user?.role}).
        </p>
      </div>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {TILES.filter((t) => !t.capability || allows(user?.role, t.capability)).map((tile) => (
          <Link key={tile.to} to={tile.to === "/content" ? "/content-types" : tile.to}>
            <Card className="hover:border-primary/40 h-full transition-colors">
              <CardHeader>
                <tile.icon className="text-primary size-6" aria-hidden="true" />
                <CardTitle>{tile.title}</CardTitle>
                <CardDescription>{tile.description}</CardDescription>
              </CardHeader>
              <CardContent />
            </Card>
          </Link>
        ))}
      </div>
    </div>
  );
}

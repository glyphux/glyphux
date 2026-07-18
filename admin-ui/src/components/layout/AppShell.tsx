import { useEffect, useState, type ReactNode } from "react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { FileText, Image, LayoutDashboard, LogOut, Menu, Shapes, Users, X } from "lucide-react";
import { useAuth } from "@/lib/auth-context";
import { useContentTypes } from "@/lib/use-content-types";
import { allows } from "@/lib/permissions";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { ThemeToggle } from "./ThemeToggle";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

function NavItem({ to, children, onClick }: { to: string; children: ReactNode; onClick?: () => void }) {
  return (
    <NavLink
      to={to}
      end={to === "/"}
      onClick={onClick}
      className={({ isActive }) =>
        cn(
          "text-body flex items-center gap-2 rounded-md px-3 py-2 font-medium transition-colors",
          isActive
            ? "bg-primary/10 text-primary"
            : "text-sidebar-foreground/80 hover:bg-sidebar-border/60 hover:text-sidebar-foreground",
        )
      }
    >
      {children}
    </NavLink>
  );
}

function SidebarNav({ onNavigate }: { onNavigate?: () => void }) {
  const { user } = useAuth();
  const { types } = useContentTypes();

  return (
    <nav className="flex flex-col gap-1 p-3" aria-label="Primary">
      <NavItem to="/" onClick={onNavigate}>
        <LayoutDashboard className="size-4" /> Dashboard
      </NavItem>
      <NavItem to="/content-types" onClick={onNavigate}>
        <Shapes className="size-4" /> Content types
      </NavItem>

      {Object.keys(types).length > 0 && (
        <div className="mt-4 flex flex-col gap-1">
          <p className="text-muted-foreground px-3 text-small font-semibold tracking-wide uppercase">Content</p>
          {Object.keys(types)
            .sort()
            .map((name) => (
              <NavItem key={name} to={`/content/${name}`} onClick={onNavigate}>
                <FileText className="size-4" /> {name}
              </NavItem>
            ))}
        </div>
      )}

      <div className="mt-4 flex flex-col gap-1">
        <NavItem to="/media" onClick={onNavigate}>
          <Image className="size-4" /> Media
        </NavItem>
        {allows(user?.role, "users:manage") && (
          <NavItem to="/users" onClick={onNavigate}>
            <Users className="size-4" /> Users
          </NavItem>
        )}
      </div>
    </nav>
  );
}

export function AppShell() {
  const { user, logout } = useAuth();
  const navigate = useNavigate();
  const [mobileOpen, setMobileOpen] = useState(false);

  useEffect(() => {
    if (!mobileOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMobileOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [mobileOpen]);

  const handleLogout = async () => {
    try {
      await logout();
    } catch {
      // logout() already clears local session state in its own `finally`
      // even if the server call fails (e.g. offline) — the user is signed
      // out locally either way, so still navigate rather than leaving them
      // stranded on a page an unauthenticated user can no longer use.
    }
    navigate("/login", { replace: true });
  };

  const initials = user?.email.slice(0, 2).toUpperCase() ?? "?";

  return (
    <div className="flex min-h-svh">
      {/* Desktop sidebar */}
      <aside className="bg-sidebar border-sidebar-border hidden w-60 shrink-0 border-r md:block">
        <div className="border-sidebar-border flex h-14 items-center border-b px-4">
          <span className="text-h2 font-semibold">Glyphux</span>
        </div>
        <SidebarNav />
      </aside>

      {/* Mobile sidebar (overlay drawer) */}
      {mobileOpen && (
        <div className="fixed inset-0 z-40 md:hidden">
          <button
            aria-label="Close navigation"
            className="animate-fade-in fixed inset-0 bg-black/50"
            onClick={() => setMobileOpen(false)}
          />
          <aside className="bg-sidebar border-sidebar-border animate-slide-in fixed inset-y-0 left-0 w-64 border-r">
            <div className="border-sidebar-border flex h-14 items-center justify-between border-b px-4">
              <span className="text-h2 font-semibold">Glyphux</span>
              <Button variant="ghost" size="icon" aria-label="Close navigation" onClick={() => setMobileOpen(false)}>
                <X />
              </Button>
            </div>
            <SidebarNav onNavigate={() => setMobileOpen(false)} />
          </aside>
        </div>
      )}

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="border-border bg-background sticky top-0 z-30 flex h-14 items-center justify-between border-b px-4">
          <Button
            variant="ghost"
            size="icon"
            className="md:hidden"
            aria-label="Open navigation"
            onClick={() => setMobileOpen(true)}
          >
            <Menu />
          </Button>
          <div className="flex-1" />
          <div className="flex items-center gap-2">
            <ThemeToggle />
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" className="gap-2 px-2">
                  <Avatar className="size-7">
                    <AvatarFallback>{initials}</AvatarFallback>
                  </Avatar>
                  <span className="hidden text-small sm:inline">{user?.email}</span>
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuLabel>
                  <div className="flex flex-col">
                    <span className="font-medium">{user?.email}</span>
                    <span className="text-muted-foreground text-small capitalize">{user?.role}</span>
                  </div>
                </DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={handleLogout} variant="destructive">
                  <LogOut /> Log out
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </header>
        <main className="flex-1 p-4 sm:p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

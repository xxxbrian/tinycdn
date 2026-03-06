import type { ReactNode } from "react";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

export function PlaceholderPanel(props: {
  title: string;
  description: string;
  children?: ReactNode;
}) {
  return (
    <Card className="border-dashed">
      <CardHeader>
        <CardTitle>{props.title}</CardTitle>
        <CardDescription>{props.description}</CardDescription>
      </CardHeader>
      {props.children ? <CardContent>{props.children}</CardContent> : null}
    </Card>
  );
}

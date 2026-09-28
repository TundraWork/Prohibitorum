import { createFileRoute } from "@tanstack/react-router";
import { VrchatProofPage } from "@/pages/public/VrchatProof";

/**
 * Where a verification link in someone's VRChat bio leads. The page only
 * explains what the link is: it reads nothing, and the proof in the path is
 * never shown or sent anywhere.
 */
export const Route = createFileRoute("/_public/verify/vrchat/$proof")({
  component: VrchatProofPage,
});

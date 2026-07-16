// VerifiedBadge renders the seller trust badge. Verified → green check; pending
// → amber; anything else → nothing (unverified sellers show no badge).
export function VerifiedBadge({ status }: { status?: string | null }) {
  if (status === "verified") {
    return (
      <span className="badge badge-success" title="Domain-verified seller">
        ✓ Verified
      </span>
    );
  }
  if (status === "pending") {
    return (
      <span className="badge badge-warning" title="Verification pending">
        Pending
      </span>
    );
  }
  return null;
}

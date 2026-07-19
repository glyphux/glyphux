package notifications

import "fmt"

// renderWelcome builds the subject/body for a new-account welcome message.
func renderWelcome(e UserCreatedEvent) (subject, body string) {
	name := e.Name
	if name == "" {
		name = e.Email
	}
	return "Welcome!", fmt.Sprintf("Hi %s, your account has been created.", name)
}

// renderPaymentReceipt builds the subject/body for a completed-payment
// receipt.
func renderPaymentReceipt(e PaymentCompletedEvent) (subject, body string) {
	return "Payment received", fmt.Sprintf("We received your payment of %s for order %s.", e.Amount, e.OrderID)
}

// renderMembershipStarted builds the subject/body for a new-membership
// welcome message.
func renderMembershipStarted(e MembershipStartedEvent) (subject, body string) {
	return "Welcome to " + e.PlanName, fmt.Sprintf("Your %s membership is now active.", e.PlanName)
}

// renderMembershipExpired builds the subject/body for a membership-expiry
// notice.
func renderMembershipExpired(e MembershipExpiredEvent) (subject, body string) {
	return "Your membership has expired", fmt.Sprintf("Your %s membership has expired. Renew to keep your access.", e.PlanName)
}

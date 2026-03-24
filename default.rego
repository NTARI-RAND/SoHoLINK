package soholink.authz

# Default authorization policy for SoHoLINK AAA
# Allow any authenticated, non-revoked user network access

default allow = false

# Allow authenticated users
allow if {
    input.user != ""
    input.did != ""
    input.authenticated == true
}

# Deny reasons for debugging
deny_reasons contains reason if {
    input.user == ""
    reason := "no_username"
}

deny_reasons contains reason if {
    input.did == ""
    reason := "no_did"
}

deny_reasons contains reason if {
    input.authenticated != true
    reason := "not_authenticated"
}

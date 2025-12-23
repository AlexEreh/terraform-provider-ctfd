terraform {
  required_providers {
    ctfd = {
      source = "registry.terraform.io/AlexEreh/ctfd"
    }
  }
}

provider "ctfd" {
  url = "http://localhost:8080"
}

resource "ctfd_challenge_dynamic" "idor_challenge" {
  name        = "IDOR Rendezvous"
  category    = "web"
  description = <<-EOT
        Sample description for IDOR Rendezvous challenge.
    EOT
  attribution = "@alexereh"
  value       = 1000
  initial     = 1000
  decay       = 100
  minimum     = 100
  state       = "visible"
  function    = "logarithmic"
  flag_type   = "case_insensitive"
  flag        = "newyearctf{1D0R_1S_CL@551C}"

  topics = [
    "IDOR"
  ]
  tags = [
    "web",
    "http"
  ]
}

resource "ctfd_hint" "idor_challenge_hint_1" {
  challenge_id = ctfd_challenge_dynamic.idor_challenge.id
  content      = "Les flux http ne sont pas chiffrés"
  cost         = 50
}

resource "ctfd_hint" "idor_challenge_hint_2" {
  challenge_id = ctfd_challenge_dynamic.idor_challenge.id
  content      = "Les informations sont POSTées en HTTP :)"
  cost         = 50
  requirements = [ctfd_hint.http_hint_1.id]
}

resource "ctfd_file" "idor_challenge_file" {
  challenge_id = ctfd_challenge_dynamic.idor_challenge.id
  name         = "capture.pcapng"
  contentb64   = filebase64("${path.module}/capture.pcapng")
}



resource "ctfd_challenge_dynamic" "icmp_challenge" {
  name        = "Stealing data"
  category    = "forensics"
  description = <<-EOT
        Sample description
    EOT
  attribution = "@alexereh"
  value       = 1000
  decay       = 100
  minimum     = 100
  state       = "visible"
  requirements = {
    behavior      = "anonymized"
    prerequisites = [ctfd_challenge_dynamic.idor_challenge.id]
  }

  flags = [{
    content = "newyearctf{1CMP}"
  }]

  topics = [
    "Wireshark"
  ]
  tags = [
    "web",
    "icmp"
  ]
}

resource "ctfd_hint" "icmp_challenge_hint_1" {
  challenge_id = ctfd_challenge_dynamic.icmp_challenge.id
  content      = "Vous ne trouvez pas qu'il ya beaucoup de requêtes ICMP ?"
  cost         = 50
}

resource "ctfd_hint" "icmp_challenge_hint_2" {
  challenge_id = ctfd_challenge_dynamic.icmp_challenge.id
  content      = "Pour l'exo, le ttl a été modifié, tente un `ip.ttl<=20`"
  cost         = 50
  requirements = [ctfd_hint.icmp_hint_2.id]
}

resource "ctfd_file" "icmp_challenge_file" {
  challenge_id = ctfd_challenge_dynamic.icmp_challenge.id
  name         = "icmp.pcap"
  contentb64   = filebase64("${path.module}/icmp.pcap")
}

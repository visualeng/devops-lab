output "server_ip" {
  description = "Публичный адрес сервера"
  value       = hcloud_server.devops_lab.ipv4_address
}

output "ssh_command" {
  description = "Готовая команда входа на сервер"
  value       = "ssh root@${hcloud_server.devops_lab.ipv4_address}"
}

output "firewall_id" {
  description = "id облачного файрвола"
  value       = hcloud_firewall.devops_lab.id
}

# Стенд целиком описывается кодом: адрес, размер, доступ по ssh и
# правила файрвола. Ничего не настраивается руками в панели.

# Ключ, заведённый в Hetzner Cloud: сервер создаётся с ним, поэтому
# парольный вход не нужен и не заводится.
data "hcloud_ssh_key" "deploy" {
  name = var.ssh_key_name
}

# Статический адрес: пересоздание сервера не меняет точку входа,
# DNS и настройки деплоя остаются валидными.
resource "hcloud_primary_ip" "devops_lab" {
  name = "${var.server_name}-ip"
  type = "ipv4"
  # адрес привязывается к локации, а не к серверу: пересоздание машины
  # не отпускает точку входа
  location = var.location
}

# Наружу открыт только ssh (с перечисленных адресов) и http/https.
# Всё остальное закрыто: правил deny по умолчанию нет, но и «open all»
# тоже не делаем — список правил исчерпывающий.
resource "hcloud_firewall" "devops_lab" {
  name = "${var.server_name}-fw"

  dynamic "rule" {
    for_each = var.allowed_cidrs

    content {
      direction  = "in"
      protocol   = "tcp"
      port       = "22"
      source_ips = [rule.value]
    }
  }

  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "80"
    source_ips = ["0.0.0.0/0", "::/0"]
  }

  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "443"
    source_ips = ["0.0.0.0/0", "::/0"]
  }

  rule {
    direction       = "out"
    protocol        = "tcp"
    port            = "80"
    destination_ips = ["0.0.0.0/0", "::/0"]
  }

  rule {
    direction       = "out"
    protocol        = "tcp"
    port            = "443"
    destination_ips = ["0.0.0.0/0", "::/0"]
  }

  rule {
    direction       = "out"
    protocol        = "icmp"
    destination_ips = ["0.0.0.0/0", "::/0"]
  }
}

resource "hcloud_server" "devops_lab" {
  name         = var.server_name
  server_type  = var.server_type
  image        = var.image
  location     = var.location
  ssh_keys     = [data.hcloud_ssh_key.deploy.id]
  firewall_ids = [hcloud_firewall.devops_lab.id]
  keep_disk    = true

  public_net {
    ipv4_enabled = false
    ipv6_enabled = true
    ipv4         = hcloud_primary_ip.devops_lab.id
  }
}

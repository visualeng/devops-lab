variable "server_name" {
  description = "Имя сервера в Hetzner Cloud и в state"
  type        = string
  default     = "devops-lab"
}

variable "server_type" {
  description = "Тип сервера: cx22 — 2 vCPU, 4 ГБ RAM"
  type        = string
  default     = "cx22"
}

variable "location" {
  description = "Локация Hetzner Cloud"
  type        = string
  default     = "fsn1"

  validation {
    condition     = contains(["fsn1", "nbg1", "hel1", "ash", "hil"], var.location)
    error_message = "Локация должна быть одной из: fsn1, nbg1, hel1, ash, hil."
  }
}

variable "image" {
  description = "Образ ОС"
  type        = string
  default     = "ubuntu-24.04"
}

variable "ssh_key_name" {
  description = "Имя ssh-ключа, уже заведённого в Hetzner Cloud"
  type        = string
}

variable "allowed_cidrs" {
  description = "Адреса, с которых разрешён ssh. Список пустой — ssh закрыт для всех."
  type        = list(string)

  validation {
    condition     = alltrue([for cidr in var.allowed_cidrs : can(cidrnetmask(cidr))])
    error_message = "Каждый элемент должен быть корректным CIDR, например 203.0.113.10/32."
  }
}

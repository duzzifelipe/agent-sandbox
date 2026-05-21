packer {
  required_plugins {
    qemu = {
      source  = "github.com/hashicorp/qemu"
      version = "~> 1"
    }
  }
}

variable "vm_name" {
  type        = string
  description = "VM name; used as the image filename base"
}

variable "cloud_image_path" {
  type        = string
  description = "Local absolute path to the cached Ubuntu cloud image"
}

variable "seed_iso_path" {
  type        = string
  description = "Local absolute path to the cloud-init seed ISO for Packer SSH access"
}

variable "ssh_private_key_file" {
  type        = string
  description = "Local absolute path to the ephemeral private key for Packer SSH"
}

variable "efi_firmware_vars" {
  type        = string
  default     = ""
  description = "Path to a writable EDK2 vars file (ARM64 only); empty string skips this drive"
}

variable "provision_script" {
  type        = string
  description = "Local absolute path to the generated orchestration shell script"
}

variable "output_dir" {
  type        = string
  description = "Directory where the built qcow2 image will be written"
}

variable "qemu_binary" {
  type    = string
  default = "qemu-system-aarch64"
}

variable "machine_type" {
  type    = string
  default = "virt"
}

variable "cpu_model" {
  type    = string
  default = "host"
}

variable "efi_firmware_code" {
  type        = string
  default     = "/opt/homebrew/share/qemu/edk2-aarch64-code.fd"
  description = "Path to EDK2 ARM64 firmware (read-only); empty string skips this drive"
}

locals {
  efi_code_args = var.efi_firmware_code != "" ? [
    ["-drive", "if=pflash,format=raw,readonly=on,file=${var.efi_firmware_code}"]
  ] : []
  efi_vars_args = var.efi_firmware_vars != "" ? [
    ["-drive", "if=pflash,format=raw,file=${var.efi_firmware_vars}"]
  ] : []
  seed_args    = [["-drive", "file=${var.seed_iso_path},format=raw,if=virtio"]]
  serial_args  = [["-serial", "file:/tmp/agentsdx-vm-serial.log"]]
  all_qemuargs = concat(local.efi_code_args, local.efi_vars_args, local.seed_args, local.serial_args)
}

source "qemu" "vm" {
  vm_name              = var.vm_name
  iso_url              = var.cloud_image_path
  iso_checksum         = "none"
  disk_image           = true
  qemu_binary          = var.qemu_binary
  machine_type         = var.machine_type
  cpu_model            = var.cpu_model
  accelerator          = "hvf"
  disk_size            = "20480M"
  memory               = 2048
  cpus                 = 2
  headless             = true
  vnc_password         = "demo"
  ssh_username         = "root"
  ssh_private_key_file = var.ssh_private_key_file
  ssh_timeout          = "10m"
  shutdown_command     = "shutdown -P now"

  qemuargs = local.all_qemuargs

  output_directory = var.output_dir
  format           = "qcow2"
}

build {
  sources = ["source.qemu.vm"]

  provisioner "file" {
    source      = "${path.root}/"
    destination = "/tmp/agentsdx-vm/"
  }

  provisioner "shell" {
    script = var.provision_script
  }
}

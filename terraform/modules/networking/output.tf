output "vpc_id" {
  value = aws_vpc.vpc.id
}

output "public_subnet_ids" {
  value = [
    aws_subnet.public_subnet_one.id,
    aws_subnet.public_subnet_two.id,
  ]
}

output "private_subnet_ids" {
  value = [
    aws_subnet.private_subnet_one.id,
    aws_subnet.private_subnet_two.id,
  ]
}

output "private_route_table_ids" {
  value = [
    aws_route_table.private_route_table_one.id,
    aws_route_table.private_route_table_two.id,
  ]
}

output "nat_gateway_one_ipv4" {
  value = aws_eip.nat_gateway_one_attachment.public_ip
}

output "nat_gateway_two_ipv4" {
  value = aws_eip.nat_gateway_two_attachment.public_ip
}

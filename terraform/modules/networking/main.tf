data "aws_region" "current" {}

resource "aws_vpc" "vpc" {
  cidr_block = var.vpc_cidr

  tags = merge(var.additional_tags, {
    Name = "media-service-vpc"
  })
}

resource "aws_subnet" "public_subnet_one" {
  vpc_id                  = aws_vpc.vpc.id
  cidr_block              = var.public_subnet_cidrs[0]
  availability_zone       = "${data.aws_region.current.name}a"
  map_public_ip_on_launch = true

  tags = merge(var.additional_tags, {
    Name = "media-service-public-subnet-one"
  })
}

resource "aws_subnet" "public_subnet_two" {
  vpc_id                  = aws_vpc.vpc.id
  cidr_block              = var.public_subnet_cidrs[1]
  availability_zone       = "${data.aws_region.current.name}b"
  map_public_ip_on_launch = true

  tags = merge(var.additional_tags, {
    Name = "media-service-public-subnet-two"
  })
}

resource "aws_subnet" "private_subnet_one" {
  vpc_id            = aws_vpc.vpc.id
  cidr_block        = var.private_subnet_cidrs[0]
  availability_zone = "${data.aws_region.current.name}a"

  tags = merge(var.additional_tags, {
    Name = "media-service-private-subnet-one"
  })
}

resource "aws_subnet" "private_subnet_two" {
  vpc_id            = aws_vpc.vpc.id
  cidr_block        = var.private_subnet_cidrs[1]
  availability_zone = "${data.aws_region.current.name}b"

  tags = merge(var.additional_tags, {
    Name = "media-service-private-subnet-two"
  })
}

resource "aws_internet_gateway" "internet_gateway" {
  tags = merge(var.additional_tags, {
    Name = "media-service-internet-gateway"
  })
}

resource "aws_internet_gateway_attachment" "internet_gateway_attachment" {
  internet_gateway_id = aws_internet_gateway.internet_gateway.id
  vpc_id              = aws_vpc.vpc.id
}

resource "aws_route_table" "public_route_table" {
  vpc_id     = aws_vpc.vpc.id
  depends_on = [aws_vpc.vpc]

  tags = merge(var.additional_tags, {
    Name = "media-service-public-route-table"
  })
}

resource "aws_route" "public_route" {
  route_table_id         = aws_route_table.public_route_table.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.internet_gateway.id
}

resource "aws_route_table_association" "public_subnet_one_association" {
  subnet_id      = aws_subnet.public_subnet_one.id
  route_table_id = aws_route_table.public_route_table.id
}

resource "aws_route_table_association" "public_subnet_two_association" {
  subnet_id      = aws_subnet.public_subnet_two.id
  route_table_id = aws_route_table.public_route_table.id
}

resource "aws_eip" "nat_gateway_one_attachment" {
  domain = "vpc"

  tags = merge(var.additional_tags, {
    Name = "media-service-nat-gateway-one"
  })
}

resource "aws_eip" "nat_gateway_two_attachment" {
  domain = "vpc"

  tags = merge(var.additional_tags, {
    Name = "media-service-nat-gateway-two"
  })
}

resource "aws_nat_gateway" "nat_gateway_one" {
  allocation_id = aws_eip.nat_gateway_one_attachment.allocation_id
  subnet_id     = aws_subnet.public_subnet_one.id

  tags = merge(var.additional_tags, {
    Name = "media-service-nat-gateway-one"
  })
}

resource "aws_nat_gateway" "nat_gateway_two" {
  allocation_id = aws_eip.nat_gateway_two_attachment.allocation_id
  subnet_id     = aws_subnet.public_subnet_two.id

  tags = merge(var.additional_tags, {
    Name = "media-service-nat-gateway-two"
  })
}

resource "aws_route_table" "private_route_table_one" {
  vpc_id     = aws_vpc.vpc.id
  depends_on = [aws_vpc.vpc]

  tags = merge(var.additional_tags, {
    Name = "media-service-private-route-table-one"
  })
}

resource "aws_route" "private_route_one" {
  route_table_id         = aws_route_table.private_route_table_one.id
  destination_cidr_block = "0.0.0.0/0"
  nat_gateway_id         = aws_nat_gateway.nat_gateway_one.id
  depends_on             = [aws_nat_gateway.nat_gateway_one]
}

resource "aws_route_table_association" "private_route_table_one_association" {
  route_table_id = aws_route_table.private_route_table_one.id
  subnet_id      = aws_subnet.private_subnet_one.id
  depends_on     = [aws_route.private_route_one]
}

resource "aws_route_table" "private_route_table_two" {
  vpc_id     = aws_vpc.vpc.id
  depends_on = [aws_vpc.vpc]

  tags = merge(var.additional_tags, {
    Name = "media-service-private-route-table-two"
  })
}

resource "aws_route" "private_route_two" {
  route_table_id         = aws_route_table.private_route_table_two.id
  destination_cidr_block = "0.0.0.0/0"
  nat_gateway_id         = aws_nat_gateway.nat_gateway_two.id
  depends_on             = [aws_nat_gateway.nat_gateway_two]
}

resource "aws_route_table_association" "private_route_table_two_association" {
  route_table_id = aws_route_table.private_route_table_two.id
  subnet_id      = aws_subnet.private_subnet_two.id
  depends_on     = [aws_route.private_route_two]
}

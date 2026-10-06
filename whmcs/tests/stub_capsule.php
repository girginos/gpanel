<?php
/** WHMCS\Database\Capsule saplamasi: bellek ici tablolar + gereken sorgu zinciri. */

namespace WHMCS\Database;

class FakeBlueprint
{
    public function __call($n, $a)
    {
        return $this;
    }
}

class FakeSchema
{
    public function hasTable($n)
    {
        return isset(Capsule::$tablolar[$n]);
    }

    public function create($n, $cb)
    {
        Capsule::$tablolar[$n] = [];
        $cb(new FakeBlueprint());
    }
}

class FakeQuery
{
    private $ad;
    private $suzgec = [];
    private $limit = null;
    private $sira = null;

    public function __construct($ad)
    {
        $this->ad = $ad;
        if (!isset(Capsule::$tablolar[$ad])) {
            Capsule::$tablolar[$ad] = [];
        }
    }

    public function where($c, $op = null, $v = null)
    {
        if (func_num_args() === 2) {
            $v = $op;
            $op = '=';
        }
        $this->suzgec[] = [$c, $op, $v];
        return $this;
    }

    public function whereIn($c, $liste)
    {
        $this->suzgec[] = [$c, 'in', $liste];
        return $this;
    }

    public function orderBy($c)
    {
        $this->sira = $c;
        return $this;
    }

    public function limit($n)
    {
        $this->limit = $n;
        return $this;
    }

    private function esles($satir)
    {
        foreach ($this->suzgec as [$c, $op, $v]) {
            $x = $satir[$c] ?? null;
            switch ($op) {
                case '=':
                    if ((string) $x !== (string) $v) {
                        return false;
                    }
                    break;
                case '!=':
                    if ((string) $x === (string) $v) {
                        return false;
                    }
                    break;
                case 'in':
                    if (!in_array($x, $v)) {
                        return false;
                    }
                    break;
            }
        }
        return true;
    }

    public function get($cols = ['*'])
    {
        Capsule::$sorgular[] = ['get', $this->ad];
        $out = [];
        foreach (Capsule::$tablolar[$this->ad] as $s) {
            if ($this->esles($s)) {
                $out[] = (object) $s;
            }
        }
        if ($this->sira) {
            $k = $this->sira;
            usort($out, function ($a, $b) use ($k) {
                return $a->$k <=> $b->$k;
            });
        }
        return $this->limit ? array_slice($out, 0, $this->limit) : $out;
    }

    public function first()
    {
        $r = $this->get();
        return $r ? $r[0] : null;
    }

    public function update(array $v)
    {
        Capsule::$sorgular[] = ['update', $this->ad];
        $n = 0;
        foreach (Capsule::$tablolar[$this->ad] as $i => $s) {
            if ($this->esles($s)) {
                Capsule::$tablolar[$this->ad][$i] = array_merge($s, $v);
                $n++;
            }
        }
        return $n;
    }

    public function insert(array $v)
    {
        Capsule::$tablolar[$this->ad][] = $v;
        return true;
    }

    public function delete()
    {
        Capsule::$tablolar[$this->ad] = array_values(array_filter(Capsule::$tablolar[$this->ad], function ($s) {
            return !$this->esles($s);
        }));
    }
}

class Capsule
{
    public static $tablolar = [];
    public static $sorgular = [];
    public static $guncellemeHatasi = []; // id => true: update istisna atar

    public static function schema()
    {
        return new FakeSchema();
    }

    public static function table($ad)
    {
        return new FakeQuery($ad);
    }

    public static function sifirla()
    {
        self::$tablolar = [];
        self::$sorgular = [];
    }
}
